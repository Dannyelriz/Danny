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
        print(f"🟢 Active Connection: {bot.user.name} (Monitoring servers)")

    @bot.event
    async def on_member_join(member):
        # Create a unique key tracking both the specific user and the server they joined
        unique_event_key = f"{member.id}-{member.guild.id}"
        
        # Check if another running account instance already logged this exact join event
        if unique_event_key in ALREADY_LOGGED:
            return  # Stop immediately, preventing duplicate embeds

        # Lock the event so no other account duplicates it
        ALREADY_LOGGED.add(unique_event_key)

        # Format a clean rich embed for the webhook destination
        payload = {
            "embeds": [
                {
                    "title": "📥 New Member Joined!",
                    "description": f"Welcome {member.mention} to the server!",
                    "color": 10181046,  # Vibrant Purple accent line (Decimal for #9B59B6)
                    
                    # 👇 This pulls their Discord profile picture and displays it on the right side
                    "thumbnail": {
                        "url": str(member.display_avatar.url)
                    },
                    
                    "fields": [
                        {"name": "Username", "value": f"{member.name}", "inline": True},
                        {"name": "Account Age", "value": f"{member.created_at.strftime('%Y-%m-%d')}", "inline": True},
                        {"name": "Server Source", "value": f"{member.guild.name}", "inline": False}
                    ],
                    "footer": {
                        "text": f"Logged via account handler: {bot.user.name}"
                    }
                }
            ]
        }
        
        try:
            requests.post(webhook_url, json=payload)
        except Exception as e:
            print(f"[{bot.user.name}] Error firing webhook response: {e}")

        # Let the background task wait 10 seconds before freeing memory cache
        await asyncio.sleep(10)
        ALREADY_LOGGED.discard(unique_event_key)

    try:
        await bot.start(token)
    except Exception as e:
        print(f"❌ Failed to log in with token [...{token[-8:]}]: {e}")

async def main():
    # Fixed the variable check name matching to BOT_CONFIGS
    if not BOT_CONFIGS:
        print("❌ CRITICAL: No valid token-webhook configurations were loaded!")
        print(f"Raw string read from environment: '{CONFIG_RAW}'")
        print("Please check that your .env file is in this folder and contains BOT_CONFIG=token|url")
        return
        
    print(f"🟢 Loaded {len(BOT_CONFIGS)} dedicated pair(s). Launching concurrent connection loops...")
    
    # Fire all bot operations simultaneously
    await asyncio.gather(*(
        launch_selfbot(config["token"], config["webhook"]) 
        for config in BOT_CONFIGS
    ))

# Fixed with proper dunder naming and straight programming quotes
if __name__ == "__main__":
    asyncio.run(main())