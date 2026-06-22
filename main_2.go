package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"
)

// Config controls which guilds are watched and where notifications go.
type Config struct {
	WatchAll   bool                   `json:"watch_all"`
	IgnoreBots bool                   `json:"ignore_bots"`
	Guilds     map[string]GuildConfig `json:"guilds"`
}

// GuildConfig is a per-guild setting.
type GuildConfig struct {
	Enabled   bool   `json:"enabled"`
	ChannelID string `json:"channel_id"`
	Label     string `json:"label,omitempty"`
}

// JoinEvent handles incoming HTTP payloads sent from your Python scripts
type JoinEvent struct {
	Username  string `json:"username"`
	UserID    string `json:"user_id"`
	AvatarURL string `json:"avatar_url"`
	GuildName string `json:"guild_name"`
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
		log.Printf("No config found at %s, created a template.", path)
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

func (c *Config) watched(guildID string) (GuildConfig, bool) {
	if gc, ok := c.Guilds[guildID]; ok {
		return gc, gc.Enabled
	}
	if c.WatchAll {
		return GuildConfig{}, true
	}
	return GuildConfig{}, false
}


// startLocalBridge starts the HTTP server that catches logs from your Python scripts
func startLocalBridge(dg *discordgo.Session, targetUserIDs []string) {
	http.HandleFunc("/join-log", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		var event JoinEvent
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		embed := &discordgo.MessageEmbed{
			Title:       "📥 New Member Joined! (Scout Link)",
			URL:         fmt.Sprintf("https://discord.com/users/%s", event.UserID),
			Description: fmt.Sprintf("👤 **%s**", event.Username),
			Color:       0x8757F2,
			Thumbnail:   &discordgo.MessageEmbedThumbnail{URL: event.AvatarURL},
			Fields: []*discordgo.MessageEmbedField{
				{Name: "Username", Value: fmt.Sprintf("`%s`", event.Username), Inline: true},
				{Name: "User ID", Value: fmt.Sprintf("`%s`", event.UserID), Inline: true},
				{Name: "Server Source", Value: event.GuildName, Inline: false},
			},
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}

		msgSend := &discordgo.MessageSend{
			Embeds: []*discordgo.MessageEmbed{embed},
		}

		for _, userID := range targetUserIDs {
			userID = strings.TrimSpace(strings.ReplaceAll(userID, "\r", ""))
			if userID == "" {
				continue
			}
			dmChannel, err := dg.UserChannelCreate(userID)
			if err == nil {
				_, sErr := dg.ChannelMessageSendComplex(dmChannel.ID, msgSend)
				if sErr != nil {
					log.Printf("[ERROR] Bridge failed to send DM to %s: %v", userID, sErr)
				}
			} else {
				log.Printf("[ERROR] Bridge failed to open DM for %s: %v", userID, err)
			}
		}
		w.WriteHeader(http.StatusOK)
	})

	go func() {
		log.Println("[INFO] Local HTTP Bridge listening on port :8080...")
		if err := http.ListenAndServe(":8080", nil); err != nil {
			log.Printf("[ERROR] HTTP Server crash: %v", err)
		}
	}()
}

func main() {
	_ = godotenv.Load()

	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		log.Fatal("BOT_TOKEN is not set. Add it to your .env file.")
	}

	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "config.json"
	}
	cfg, err := loadConfig(configPath)
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}

	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		log.Fatalf("Error creating Discord session: %v", err)
	}

	rawIDs := os.Getenv("MY_DISCORD_IDS")
	var targetUserIDs []string
	if rawIDs != "" {
		targetUserIDs = strings.Split(rawIDs, ",")
	}

	dg.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		log.Printf("[READY] Logged in as: %s#%s", r.User.Username, r.User.Discriminator)
	})

	dg.AddHandler(func(s *discordgo.Session, m *discordgo.GuildMemberAdd) {
		gc, ok := cfg.watched(m.GuildID)
		if !ok || (cfg.IgnoreBots && m.User.Bot) {
			return
		}

		guildName := m.GuildID
		if guild, gErr := s.Guild(m.GuildID); gErr == nil {
			guildName = guild.Name
		}

		userTag := m.User.Username
		if m.User.Discriminator != "" && m.User.Discriminator != "0" {
			userTag = fmt.Sprintf("%s#%s", m.User.Username, m.User.Discriminator)
		}

		embed := &discordgo.MessageEmbed{
			Title:       "📥 New Member Joined!",
			URL:         fmt.Sprintf("https://discord.com/users/%s", m.User.ID),
			Description: fmt.Sprintf("👤 **%s**", userTag),
			Color:       0x8757F2,
			Thumbnail:   &discordgo.MessageEmbedThumbnail{URL: m.User.AvatarURL("256")},
			Fields: []*discordgo.MessageEmbedField{
				{Name: "User", Value: fmt.Sprintf("`%s`", userTag), Inline: true},
				{Name: "User ID", Value: fmt.Sprintf("`%s`", m.User.ID), Inline: true},
				{Name: "Server Source", Value: guildName, Inline: false},
			},
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}

		msgSend := &discordgo.MessageSend{
			Embeds: []*discordgo.MessageEmbed{embed},
		}

		if gc.ChannelID != "" {
			_, _ = s.ChannelMessageSendComplex(gc.ChannelID, msgSend)
		}

		for _, userID := range targetUserIDs {
			userID = strings.TrimSpace(strings.ReplaceAll(userID, "\r", ""))
			if userID == "" {
				continue
			}
			dmChannel, dmErr := s.UserChannelCreate(userID)
			if dmErr == nil {
				_, sErr := s.ChannelMessageSendComplex(dmChannel.ID, msgSend)
				if sErr != nil {
					log.Printf("[ERROR] Native alert failed to send to %s: %v", userID, sErr)
				}
			}
		}
	})

	dg.Identify.Intents = discordgo.IntentsGuilds | discordgo.IntentsGuildMembers

	startLocalBridge(dg, targetUserIDs)

	if err := dg.Open(); err != nil {
		log.Fatalf("Error opening connection: %v", err)
	}
	log.Println("[INFO] Bot is now running. Press CTRL-C to exit.")

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM)
	<-sc
	log.Println("[INFO] Shutting down gracefully...")
	dg.Close()
}
