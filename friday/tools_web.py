"""
Friday Web Search Tools
Multi-engine web search with parallel execution and page fetching.
Falls back to mock results when APIs are unavailable.
"""
import logging
import urllib.parse
from concurrent.futures import ThreadPoolExecutor, as_completed
from typing import List, Optional

import requests

logger = logging.getLogger("Friday.WebTools")

USER_AGENT = "Friday/2.0 (money-maker bot; +https://github.com/friday)"
SEARCH_TIMEOUT = 15

MOCK_RESULTS = {
    "forex": [
        {"title": "Forex Market Today", "url": "https://example.com/forex", "snippet": "EURUSD at 1.0850, up 0.2% on session. Key resistance at 1.0900."},
        {"title": "Best Forex Strategies 2026", "url": "https://example.com/forex-strategies", "snippet": "Scalping, swing trading, and position trading strategies for modern markets."},
    ],
    "crypto": [
        {"title": "Bitcoin Price Analysis", "url": "https://example.com/btc", "snippet": "BTC at $67,420 with strong support at $65,000. Resistance at $70,000."},
        {"title": "Crypto Arbitrage Opportunities", "url": "https://example.com/crypto-arb", "snippet": "Price gaps between exchanges present 2-5% arbitrage opportunities."},
    ],
    "trading": [
        {"title": "Algorithmic Trading Guide", "url": "https://example.com/algo-trading", "snippet": "Build profitable trading bots with Python and machine learning."},
        {"title": "Stock Market News", "url": "https://example.com/stocks", "snippet": "Markets mixed ahead of Fed decision. S&P 500 flat."},
    ],
    "affiliate": [
        {"title": "Affiliate Marketing 2026", "url": "https://example.com/affiliate", "snippet": "Top affiliate programs paying 30-50% commission in 2026."},
        {"title": "Passive Income Ideas", "url": "https://example.com/passive-income", "snippet": "Generate $5,000/month passive income with these 10 methods."},
    ],
    "earning": [
        {"title": "Make Money Online 2026", "url": "https://example.com/earn", "snippet": "Proven methods to earn from home: freelancing, trading, content creation."},
        {"title": "Side Hustle Ideas", "url": "https://example.com/side-hustle", "snippet": "Best side hustles that pay $50-200 per day with minimal startup."},
    ],
}

MOCK_FALLBACK = [
    {"title": "Search Result", "url": "https://example.com/result", "snippet": "Information about your query from our knowledge base."},
]


def _get_mock_results(query: str) -> list:
    query_lower = query.lower()
    for key, results in MOCK_RESULTS.items():
        if key in query_lower:
            return results
    return MOCK_FALLBACK


def _search_duckduckgo(query: str, num_results: int) -> Optional[list]:
    try:
        url = "https://api.duckduckgo.com/"
        params = {"q": query, "format": "json", "no_html": 1, "skip_disambig": 1}
        headers = {"User-Agent": USER_AGENT}
        r = requests.get(url, params=params, headers=headers, timeout=SEARCH_TIMEOUT)
        if r.status_code != 200:
            return None
        data = r.json()
        results = []
        for topic in data.get("RelatedTopics", []):
            if "Text" in topic and "FirstURL" in topic:
                results.append({
                    "title": topic.get("Text", "").split(" - ")[0],
                    "url": topic.get("FirstURL", ""),
                    "snippet": topic.get("Text", ""),
                })
            if len(results) >= num_results:
                break
        return results if results else None
    except Exception as e:
        logger.debug("DuckDuckGo search failed: %s", e)
        return None


def _search_wikipedia(query: str, num_results: int) -> Optional[list]:
    try:
        params = {
            "action": "query",
            "list": "search",
            "srsearch": query,
            "format": "json",
            "srlimit": num_results,
        }
        headers = {"User-Agent": USER_AGENT}
        r = requests.get(
            "https://en.wikipedia.org/w/api.php",
            params=params, headers=headers, timeout=SEARCH_TIMEOUT
        )
        if r.status_code != 200:
            return None
        data = r.json()
        results = []
        for item in data.get("query", {}).get("search", []):
            results.append({
                "title": item.get("title", ""),
                "url": f"https://en.wikipedia.org/wiki/{urllib.parse.quote(item.get('title', ''))}",
                "snippet": item.get("snippet", "").replace("<span class=\"searchmatch\">", "").replace("</span>", ""),
            })
        return results if results else None
    except Exception as e:
        logger.debug("Wikipedia search failed: %s", e)
        return None


def _search_google_html(query: str, num_results: int) -> Optional[list]:
    try:
        headers = {
            "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
            "Accept-Language": "en-US,en;q=0.9",
        }
        params = {"q": query, "num": num_results}
        r = requests.get(
            "https://www.google.com/search",
            params=params, headers=headers, timeout=SEARCH_TIMEOUT
        )
        if r.status_code != 200:
            return None
        from html.parser import HTMLParser

        class SearchParser(HTMLParser):
            def __init__(self):
                super().__init__()
                self.results = []
                self._capture = None
                self._current = {}

            def handle_starttag(self, tag, attrs):
                attrs_dict = dict(attrs)
                if tag == "a" and attrs_dict.get("href", "").startswith("/url?q="):
                    url = attrs_dict["href"][7:]
                    url = urllib.parse.unquote(url.split("&")[0])
                    self._current["url"] = url
                    self._capture = "title"
                elif tag == "div" and "BNeawe" in attrs_dict.get("class", "") and self._capture:
                    self._capture = "snippet"

            def handle_data(self, data):
                if self._capture == "title" and "url" in self._current:
                    self._current["title"] = data
                    self._capture = None
                elif self._capture == "snippet":
                    self._current["snippet"] = data
                    self._capture = None
                    if "title" in self._current and "url" in self._current:
                        self.results.append(self._current)
                        self._current = {}
                        self._capture = None

        parser = SearchParser()
        parser.feed(r.text)
        return parser.results[:num_results] if parser.results else None
    except Exception as e:
        logger.debug("Google HTML search failed: %s", e)
        return None


def web_search(query: str, num_results: int = 8) -> list:
    engines = [
        _search_duckduckgo,
        _search_wikipedia,
    ]

    for engine in engines:
        results = engine(query, num_results)
        if results:
            return results[:num_results]

    return _get_mock_results(query)[:num_results]


def web_search_parallel(queries: list[str], num_results: int = 5) -> list[list]:
    with ThreadPoolExecutor(max_workers=len(queries)) as executor:
        futures = {executor.submit(web_search, q, num_results): q for q in queries}
        results_map = {}
        for future in as_completed(futures):
            q = futures[future]
            try:
                results_map[q] = future.result()
            except Exception as e:
                logger.error("Parallel search failed for '%s': %s", q, e)
                results_map[q] = _get_mock_results(q)[:num_results]
    return [results_map.get(q, []) for q in queries]


def fetch_page(url: str) -> str:
    try:
        headers = {"User-Agent": USER_AGENT}
        r = requests.get(url, headers=headers, timeout=SEARCH_TIMEOUT)
        r.raise_for_status()
        from html.parser import HTMLParser

        class TextExtractor(HTMLParser):
            def __init__(self):
                super().__init__()
                self.text_parts = []
                self._skip = False

            def handle_starttag(self, tag, attrs):
                if tag in ("script", "style", "noscript"):
                    self._skip = True

            def handle_endtag(self, tag):
                if tag in ("script", "style", "noscript"):
                    self._skip = False
                if tag in ("p", "br", "h1", "h2", "h3", "li"):
                    self.text_parts.append("\n")

            def handle_data(self, data):
                if not self._skip:
                    text = data.strip()
                    if text:
                        self.text_parts.append(text)

        extractor = TextExtractor()
        extractor.feed(r.text)
        text = " ".join(extractor.text_parts)
        lines = [line.strip() for line in text.split("\n") if line.strip()]
        return "\n".join(lines[:200])
    except Exception as e:
        logger.error("fetch_page failed for %s: %s", url, e)
        return f"Error fetching page: {e}"
