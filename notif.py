import os
import asyncio
import discord
from discord.ext import commands
import requests
from dotenv import load_dotenv

# Load variables from your local .env file
load_dotenv()
CONFIG_RAW = os.getenv("BOT_CONFIG", "")

# Shared cache memory checklist to track and eliminate duplicate logs
ALREADY_LOGGED = set()

def parse_config(config_str):
    """Parses the 'TOKEN|WEBHOOK,TOKEN|WEBHOOK' string into a list of dicts"""
    pairs = []
    if not config_str:
        return pairs
        
    for item in config_str.split(","):
        if "|" in item:
            token, webhook = item.split("|", 1)
            pairs.append({
                "token": token.strip(),
                "webhook": webhook.strip()
            })
    return pairs

# Process the string into our active array list
BOT_CONFIGS = parse_config(CONFIG_RAW)

async def launch_selfbot(token, webhook_url):
    # Initialize the selfbot instance safely
    bot = commands.Bot(command_prefix="!", self_bot=True)

    @bot.event
    async def on_ready():
        print(f"🟢 Scout Active: {bot.user.name} (Forwarding joins to Go Bridge)")

    @bot.event
    async def on_member_join(member):
        # Create a unique key tracking both the specific user and the server they joined
        unique_event_key = f"{member.id}-{member.guild.id}"
        
        # Check if another running account instance already logged this exact join event
        if unique_event_key in ALREADY_LOGGED:
            return  # Stop immediately, preventing duplicate embeds

        # Lock the event so no other account duplicates it
        ALREADY_LOGGED.add(unique_event_key)

        # Pack the raw data to send to your Go bot locally
        payload = {
            "username": member.name,
            "user_id": str(member.id),
            "avatar_url": str(member.display_avatar.url),
            "guild_name": member.guild.name
        }

        try:
            # Send the data instantly to your main Go bot running on port 8080
            requests.post("http://127.0.0.1:8080/join-log", json=payload)
        except Exception as e:
            print(f"[{bot.user.name}] Failed to forward data to Go bot bridge: {e}")

        # Let the background task wait 10 seconds before freeing memory cache
        await asyncio.sleep(10)
        ALREADY_LOGGED.discard(unique_event_key)

    try:
        await bot.start(token)
    except Exception as e:
        print(f"❌ Failed to log in with token [...{token[-8:]}]: {e}")

async def main():
    if not BOT_CONFIGS:
        print("❌ CRITICAL: No valid token-webhook configurations were loaded!")
        return
        
    print(f"🟢 Loaded {len(BOT_CONFIGS)} dedicated pair(s). Launching concurrent connection loops...")
    
    # Fire all bot operations simultaneously
    await asyncio.gather(*(
        launch_selfbot(config["token"], config["webhook"]) 
        for config in BOT_CONFIGS
    ))

if __name__ == "__main__":
    asyncio.run(main())