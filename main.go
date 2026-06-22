package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"
)

// Config controls which guilds are watched and where notifications go.
// It's loaded from a JSON file (CONFIG_PATH env var, default "config.json").
// If the file doesn't exist, a template is written so it can be edited.
type Config struct {
	// WatchAll, if true, watches every guild the bot is in unless a guild
	// has an explicit "enabled": false override in Guilds.
	WatchAll bool `json:"watch_all"`

	// IgnoreBots skips notifications for accounts flagged as bots.
	IgnoreBots bool `json:"ignore_bots"`

	// Guilds holds per-guild settings, keyed by guild (server) ID.
	Guilds map[string]GuildConfig `json:"guilds"`
}

// GuildConfig is a per-guild setting.
type GuildConfig struct {
	// Enabled turns notifications on/off for this specific guild.
	Enabled bool `json:"enabled"`

	// ChannelID is the channel the "new member" message gets posted to.
	// REQUIRED for any guild you want notifications from.
	ChannelID string `json:"channel_id"`

	// Label, if set, replaces the guild's real name in notification
	// messages.
	Label string `json:"label,omitempty"`
}

func defaultConfig() *Config {
	return &Config{
		WatchAll:   false,
		IgnoreBots: true,
		Guilds: map[string]GuildConfig{
			"1516878042730070227": {
				Enabled:   true,
				ChannelID: "1517515433660383262",
				Label:     "",
			},
		},
	}
}

// loadConfig reads the config file at path, creating a template if it
// doesn't exist yet.
func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		cfg := defaultConfig()
		out, mErr := json.MarshalIndent(cfg, "", "  ")
		if mErr != nil {
			return nil, fmt.Errorf("marshal default config: %w", mErr)
		}
		if wErr := os.WriteFile(path, out, 0o600); wErr != nil {
			return nil, fmt.Errorf("write default config: %w", wErr)
		}
		log.Printf("No config found at %s, created a template. Edit it with your real server/channel IDs, then run again.", path)
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if cfg.Guilds == nil {
		cfg.Guilds = map[string]GuildConfig{}
	}
	return &cfg, nil
}

// watched reports whether the guild should trigger notifications, and
// returns its settings (channel ID, label) if so.
func (c *Config) watched(guildID string) (GuildConfig, bool) {
	if gc, ok := c.Guilds[guildID]; ok {
		return gc, gc.Enabled
	}
	if c.WatchAll {
		return GuildConfig{}, true
	}
	return GuildConfig{}, false
}

// displayName returns the configured label for a guild, falling back to
// the real name if no override is set.
func (c *Config) displayName(guildID, realName string) string {
	if gc, ok := c.Guilds[guildID]; ok && gc.Label != "" {
		return gc.Label
	}
	return realName
}

// buildJoinEmbed constructs the embed posted to the channel when someone
// joins. Colors are standard Discord hex values; feel free to change them.
func buildJoinEmbed(cfg *Config, m *discordgo.GuildMemberAdd, guildName string, joinedAt time.Time) *discordgo.MessageEmbed {
	// Discord phased out discriminators ("#0001") for most accounts; new
	// usernames show a "0" discriminator, in which case we just show the
	// plain username instead of "name#0".
	userTag := m.User.Username
	if m.User.Discriminator != "" && m.User.Discriminator != "0" {
		userTag = fmt.Sprintf("%s#%s", m.User.Username, m.User.Discriminator)
	}

	return &discordgo.MessageEmbed{
		Title:     "New Member Joined",
		Color:     0x8757F2, // purple
		Thumbnail: &discordgo.MessageEmbedThumbnail{URL: m.User.AvatarURL("256")},
		Fields: []*discordgo.MessageEmbedField{
			{Name: "User", Value: userTag, Inline: true},
			{Name: "Mention", Value: fmt.Sprintf("<@%s>", m.User.ID), Inline: true},
			{Name: "User ID", Value: m.User.ID, Inline: false},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: cfg.displayName(m.GuildID, guildName),
		},
		Timestamp: joinedAt.Format(time.RFC3339),
	}
}

func main() {
	// Loads BOT_TOKEN (and anything else) from a ".env" file sitting
	// next to this program. If there's no .env file, this does nothing
	// and doesn't crash — it just falls through to a real env var if set.
	_ = godotenv.Load()

	// >>> INPUT NEEDED <<<
	// Your bot's secret token goes in a file named ".env" (see
	// .env.example), NEVER directly in this source file.
	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		log.Fatal("BOT_TOKEN is not set. Add it to your .env file (see .env.example).")
	}

	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "config.json"
	}
	cfg, err := loadConfig(configPath)
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}

	// Bot tokens must be sent with a "Bot " prefix — this is what makes it
	// a real bot connection instead of a self-bot. Do not remove this.
	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		log.Fatalf("Error creating Discord session: %v", err)
	}

	dg.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		log.Printf("[READY] Logged in as: %s#%s", r.User.Username, r.User.Discriminator)
		log.Printf("[READY] In %d guild(s), watch_all=%v, ignore_bots=%v", len(r.Guilds), cfg.WatchAll, cfg.IgnoreBots)
	})

	dg.AddHandler(func(s *discordgo.Session, m *discordgo.GuildMemberAdd) {
		gc, ok := cfg.watched(m.GuildID)
		if !ok {
			return
		}
		if cfg.IgnoreBots && m.User.Bot {
			return
		}
		if gc.ChannelID == "" {
			log.Printf("[WARN] Guild %s is watched but has no channel_id set in config.json — skipping", m.GuildID)
			return
		}

		guildName := m.GuildID
		if guild, gErr := s.Guild(m.GuildID); gErr == nil {
			guildName = guild.Name
		}

		joinedAt := time.Now().UTC()
		if m.Member != nil && !m.Member.JoinedAt.IsZero() {
			joinedAt = m.Member.JoinedAt.UTC()
		}

		embed := buildJoinEmbed(cfg, m, guildName, joinedAt)

		if _, sErr := s.ChannelMessageSendEmbed(gc.ChannelID, embed); sErr != nil {
			log.Printf("[ERROR] Failed to post to channel %s: %v", gc.ChannelID, sErr)
			return
		}
		log.Printf("[OK] Posted to channel %s: %s joined %s", gc.ChannelID, m.User.Username, guildName)
	})

	// Privileged intent — this must ALSO be turned on in the Developer
	// Portal under Bot > Privileged Gateway Intents > Server Members
	// Intent, or the bot will fail to connect.
	dg.Identify.Intents = discordgo.IntentsGuilds | discordgo.IntentsGuildMembers

	if err := dg.Open(); err != nil {
		log.Fatalf("Error opening connection: %v", err)
	}
	defer dg.Close()

	log.Println("Bot is running. Press CTRL+C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM)
	<-sc
	log.Println("Shutting down...")
}
