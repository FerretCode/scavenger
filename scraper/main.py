import os
import asyncio
import json
from aiohttp import web, WSMsgType
from typing import Union
from crawl4ai import AsyncWebCrawler, BrowserConfig, CrawlerRunConfig, CacheMode, LLMConfig
from crawl4ai.extraction_strategy import LLMExtractionStrategy
from apscheduler.schedulers.background import BackgroundScheduler
from apscheduler.triggers.cron import CronTrigger
from dotenv import load_dotenv

# Constants
PORT = int(os.getenv("PORT", 8080))

load_dotenv()

# Globals
latest_result = None
connected_websockets = set()
scrape_queue = asyncio.Queue()
event_loop: Union[asyncio.AbstractEventLoop, None] = None


async def websocket_handler(request):
    print("[WebSocket] Client connected")
    ws = web.WebSocketResponse()
    await ws.prepare(request)

    connected_websockets.add(ws)

    try:
        if latest_result:
            await ws.send_str(latest_result)
            print("[WebSocket] Sent cached result")

        async for msg in ws:
            if msg.type == WSMsgType.TEXT:
                pass  # Handle client messages here if needed
            elif msg.type == WSMsgType.ERROR:
                print(f"[WebSocket] Error: {ws.exception()}")
    finally:
        print("[WebSocket] Client disconnected")
        connected_websockets.remove(ws)

    return ws


async def scraper_worker(run_config):
    browser_config = BrowserConfig(verbose=True)
    crawler = AsyncWebCrawler(config=browser_config)

    try:
        await crawler.start()

        while True:
            print("[Worker] Waiting for a task")
            await scrape_queue.get()
            print("[Worker] Task received")
            try:
                global latest_result

                result = await crawler.arun(
                    url=os.environ["WEBPAGE_URL"],
                    config=run_config
                )

                latest_result = result[0].extracted_content
                print("[Scraper] Updated latest result")

                disconnected = []
                for ws in connected_websockets:
                    try:
                        await ws.send_str(latest_result)
                        print("[Broadcast] Sent to a client")
                    except Exception:
                        print("[Broadcast] Removing closed connection")
                        disconnected.append(ws)

                for ws in disconnected:
                    connected_websockets.remove(ws)

            except Exception as e:
                print(f"[Scraper] Error running scraping: {e}")
            finally:
                scrape_queue.task_done()

    finally:
        await crawler.close()


async def health_check(request):
    return web.Response(text="OK")


async def start_server():
    app = web.Application()
    app.router.add_get('/healthz', health_check)
    app.router.add_get('/ws', websocket_handler)

    runner = web.AppRunner(app)
    await runner.setup()
    site = web.TCPSite(runner, '0.0.0.0', PORT)
    await site.start()
    print(f"[App] Server running on http://0.0.0.0:{PORT}")


async def main():
    global event_loop
    event_loop = asyncio.get_running_loop()

    scheduler = BackgroundScheduler()
    cron_trigger = CronTrigger.from_crontab(os.environ["CRONTAB"])

    run_config = CrawlerRunConfig(
        word_count_threshold=1,
        extraction_strategy=LLMExtractionStrategy(
            llm_config=LLMConfig(
                provider="gemini/gemini-2.0-flash",
                api_token=os.environ["GEMINI_API_KEY"]
            ),
            schema=json.loads(os.environ["SCHEMA"]),
            extraction_type="schema",
            instruction=os.environ["PROMPT"]
        ),
        cache_mode=CacheMode.BYPASS
    )

    def schedule_scrape():
        if event_loop is None:
            return
        print("[Scheduler] Enqueueing scrape task")
        event_loop.call_soon_threadsafe(scrape_queue.put_nowait, None)

    scheduler.add_job(schedule_scrape, trigger=cron_trigger)
    scheduler.start()

    await scrape_queue.put(None)
    asyncio.create_task(scraper_worker(run_config))
    await start_server()
    await asyncio.Future()  # run forever


if __name__ == "__main__":
    asyncio.run(main())
