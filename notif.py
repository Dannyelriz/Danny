import os
import asyncio
import discord
from discord.ext import commands
import aiohttp
from dotenv import load_dotenv

load_dotenv()
CONFIG_RAW = os.getenv("BOT_CONFIG", "")
# Added dynamic BRIDGE_URL with a localhost fallback for easy local testing[cite: 2]
BRIDGE_URL = os.getenv("BRIDGE_URL", "http://127.0.0.1:8080/join-log")

ALREADY_LOGGED = set()

def parse_config(config_str):
    """Parses 'TOKEN|WEBHOOK' (No guild ID needed for this shared-server approach)"""
    pairs = []
    if not config_str:
        return pairs
        
    for item in config_str.split(","):
        parts = item.split("|")
        if len(parts) >= 2:
            pairs.append({
                "token": parts[0].strip(),
                "webhook": parts[1].strip()
            })
    return pairs

BOT_CONFIGS = parse_config(CONFIG_RAW)

async def launch_selfbot(token, webhook_url):
    bot = commands.Bot(command_prefix="!", self_bot=True)

    @bot.event
    async def on_ready():
        print(f"🟢 Scout Active: {bot.user.name} (Ready to forward)")

    @bot.event
    async def on_member_join(member):
        # 🚨 BULLETPROOF CACHE KEY: Tied directly to the destination webhook
        unique_event_key = f"{webhook_url}-{member.id}-{member.guild.id}"
        
        if unique_event_key in ALREADY_LOGGED:
            return  

        ALREADY_LOGGED.add(unique_event_key)

        # Diagnostic print to prove Python is seeing it twice
        print(f"[{bot.user.name}] 📨 Detected {member.name} joining {member.guild.name}. Forwarding to bridge...")

        payload = {
            "username": member.name,
            "user_id": str(member.id),
            "avatar_url": str(member.display_avatar.url) if member.display_avatar else "",
            "guild_name": member.guild.name,
            "target_webhook": webhook_url
        }

        try:
            async with aiohttp.ClientSession() as session:
                # Swapped hardcoded localhost to the BRIDGE_URL variable[cite: 2]
                async with session.post(BRIDGE_URL, json=payload) as response:
                    if response.status != 200:
                        print(f"[{bot.user.name}] ⚠️ Go bridge returned code: {response.status}")
        except Exception as e:
            print(f"[{bot.user.name}] ❌ Failed to connect to Go bridge: {e}")

        await asyncio.sleep(10)
        ALREADY_LOGGED.discard(unique_event_key)

    try:
        await bot.start(token)
    except Exception as e:
        print(f"❌ Failed to log in with token [...{token[-8:]}]: {e}")
        await bot.close()

async def main():
    if not BOT_CONFIGS:
        print("❌ CRITICAL: No valid configurations loaded!")
        return
        
    print(f"🟢 Loaded {len(BOT_CONFIGS)} bots. Launching...")
    
    await asyncio.gather(*(
        launch_selfbot(config["token"], config["webhook"]) 
        for config in BOT_CONFIGS
    ))

if __name__ == "__main__":
    asyncio.run(main())