import os
import asyncio
import json
from typing import Union
from crawl4ai import AsyncWebCrawler, BrowserConfig, CrawlerRunConfig, CacheMode, LLMConfig
from crawl4ai.extraction_strategy import LLMExtractionStrategy
from apscheduler.schedulers.background import BackgroundScheduler
from apscheduler.triggers.cron import CronTrigger
from dotenv import load_dotenv
from websockets.exceptions import ConnectionClosed
from websockets.asyncio.server import serve

load_dotenv()

# Globals
latest_result = None
connected_websockets = set()
scrape_queue = asyncio.Queue()
event_loop: Union[asyncio.AbstractEventLoop, None] = None


async def serve_websocket(websocket):
    print("[WebSocket] Client connected")
    connected_websockets.add(websocket)

    try:
        if latest_result:
            await websocket.send(latest_result)
            print("[WebSocket] Sent cached result")
        await websocket.wait_closed()
    finally:
        print("[WebSocket] Client disconnected")
        connected_websockets.remove(websocket)


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

                # broadcast to all connected clients
                disconnected = []
                for ws in connected_websockets:
                    try:
                        await ws.send(latest_result)
                        print("[Broadcast] Sent to a client")
                    except ConnectionClosed:
                        print("[Broadcast] Removing closed connection")
                        disconnected.append(ws)

                for ws in disconnected:
                    connected_websockets.remove(ws)

            except Exception as e:
                print(f"[Scraper] Error running scraping: {e}")
            finally:
                scrape_queue.task_done()

    finally:
        # Only close the browser when the worker stops completely
        await crawler.close()


async def main():
    global event_loop
    event_loop = asyncio.get_running_loop()

    # start scheduler
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
            return  # do not schedule a scraping job if the event loop is not assigned
        print("[Scheduler] Enqueueing scrape task")
        event_loop.call_soon_threadsafe(scrape_queue.put_nowait, None)

    scheduler.add_job(schedule_scrape, trigger=cron_trigger)
    scheduler.start()

    await scrape_queue.put(None)

    # start background scraper worker
    asyncio.create_task(scraper_worker(run_config))

    async with serve(serve_websocket, "localhost", 8765):
        print("[WebSocket] Server running on ws://localhost:8765")
        await asyncio.Future()

if __name__ == "__main__":
    asyncio.run(main())
