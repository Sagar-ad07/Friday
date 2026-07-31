"""
Friday - Trading analysis & backtesting engine.

Delegates to the trading/ module (London ORB bot + backtest).
Gives Friday real trading intelligence with prop-firm compliance math.
"""
import json
import logging
import os
import sys
from typing import Optional

logger = logging.getLogger("Friday.TradingEngine")

_TRADING_DIR = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "trading")


def _import_trading_module(name: str):
    """Import a module from the trading/ directory."""
    import importlib.util
    path = os.path.join(_TRADING_DIR, name + ".py")
    if not os.path.exists(path):
        raise ImportError(f"trading/{name}.py not found")
    spec = importlib.util.spec_from_file_location(f"trading.{name}", path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def backtest(strategy="london_orb_retest", symbol=None, timeframe="M1", days=60,
             initial_balance=None, sl_pips=8, tp_pips=16, risk_usd=18.0):
    """Run a backtest using the trading/ module. Returns JSON-serializable dict."""
    if strategy == "london_orb_retest":
        try:
            bt = _import_trading_module("backtest")
            df = bt._load_m1_data(days)
            result = bt.backtest(df, verbose=False)
            return {
                "ok": True,
                "strategy": "London ORB Retest",
                "symbol": "EURUSD",
                "timeframe": "M1",
                "days": days,
                "trades": result["trade_count"],
                "wins": result["wins"],
                "losses": result["losses"],
                "win_rate_pct": result["winrate_pct"],
                "net_profit_usd": result["total_profit"],
                "max_drawdown_pct": result["max_drawdown_pct"],
                "max_consecutive_losses": result["max_consecutive_losses"],
                "profitable_days": result["profitable_days"],
                "expectancy_usd": result["expectancy"],
                "profit_factor": round(
                    (result["wins"] * bt.REWARD_USD) /
                    max(1, result["losses"] * bt.RISK_USD), 2),
                "compliance": {
                    "under_15pct_consistency": result["avg_win"] <= 37.50,
                    "drawdown_safe": result["max_drawdown_pct"] <= 5,
                    "daily_loss_safe": result["max_consecutive_losses"] * bt.RISK_USD <= 150,
                },
                "sample_trades": [
                    {"date": t["date"], "direction": t["direction"],
                     "entry": t["entry"], "result": t["result"], "pnl": t["pnl"]}
                    for t in result["trades"][:5]
                ],
            }
        except Exception as e:
            logger.exception("ORB backtest failed")
            return {"ok": False, "error": f"ORB backtest failed: {e}"}
    else:
        # Generic bollinger-rsi backtest (existing logic)
        try:
            import MetaTrader5 as mt5
            import pandas as pd
            import numpy as np
            import math
        except ImportError as e:
            return {"ok": False, "error": f"missing dep: {e}"}

        cfg = _load_config()
        symbol = symbol or cfg.get("SYMBOL", "EURUSD")
        init = float(initial_balance or cfg.get("ACCOUNT_BALANCE", 5000.0))
        try:
            df = _fetch_data(symbol, timeframe, days)
        except Exception as e:
            return {"ok": False, "error": f"data unavailable: {e}"}

        signals, ma, lower, upper = _bollinger_rsi_signals(df)
        trades, equity, pos = [], [init], None
        pip = 0.0001 if "JPY" not in symbol else 0.01
        for i, (idx, sig) in enumerate(signals):
            price = float(df["close"].iloc[idx])
            if pos is None:
                if sig == "BUY":
                    pos = {"side": "BUY", "entry": price,
                           "sl": price - sl_pips * pip,
                           "tp": price + tp_pips * pip, "entry_idx": idx}
                elif sig == "SELL":
                    pos = {"side": "SELL", "entry": price,
                           "sl": price + sl_pips * pip,
                           "tp": price - tp_pips * pip, "entry_idx": idx}
            else:
                hi, lo = float(df["high"].iloc[idx]), float(df["low"].iloc[idx])
                exit_price, outcome = None, None
                if pos["side"] == "BUY":
                    if lo <= pos["sl"]:
                        exit_price, outcome = pos["sl"], "SL"
                    elif hi >= pos["tp"]:
                        exit_price, outcome = pos["tp"], "TP"
                    elif sig == "SELL":
                        exit_price, outcome = price, "SIGNAL"
                else:
                    if hi >= pos["sl"]:
                        exit_price, outcome = pos["sl"], "SL"
                    elif lo <= pos["tp"]:
                        exit_price, outcome = pos["tp"], "TP"
                    elif sig == "BUY":
                        exit_price, outcome = price, "SIGNAL"
                if exit_price is not None:
                    move_pips = ((exit_price - pos["entry"]) if pos["side"] == "BUY"
                                 else (pos["entry"] - exit_price)) / pip
                    pnl = risk_usd * (move_pips / sl_pips)
                    trades.append({"side": pos["side"], "entry": round(pos["entry"], 5),
                                   "exit": round(exit_price, 5), "outcome": outcome,
                                   "pnl": round(pnl, 2)})
                    equity.append(equity[-1] + pnl)
                    pos = None
        wins = [t for t in trades if t["pnl"] > 0]
        losses = [t for t in trades if t["pnl"] <= 0]
        gross_win = sum(t["pnl"] for t in wins)
        gross_loss = abs(sum(t["pnl"] for t in losses))
        win_rate = (len(wins) / len(trades) * 100) if trades else 0.0
        profit_factor = (gross_win / gross_loss) if gross_loss > 0 else (
            float("inf") if gross_win > 0 else 0)
        max_dd = 0.0
        peak = equity[0]
        for e in equity:
            peak = max(peak, e)
            max_dd = max(max_dd, peak - e)
        rets = [equity[i] - equity[i - 1] for i in range(1, len(equity))]
        sharpe = (np.mean(rets) / (np.std(rets) + 1e-9) * math.sqrt(252)) if len(rets) > 1 else 0.0
        net = equity[-1] - equity[0]
        dloss = cfg.get("DAILY_LOSS_LIMIT_PCT", 0.03) * init
        dprofit = cfg.get("DAILY_PROFIT_CAP", 30.0)
        return {
            "ok": True,
            "strategy": strategy, "symbol": symbol, "timeframe": timeframe,
            "days": days, "trades": len(trades), "net_profit_usd": round(net, 2),
            "win_rate_pct": round(win_rate, 1),
            "profit_factor": round(profit_factor, 2) if profit_factor != float("inf") else "inf",
            "max_drawdown_usd": round(max_dd, 2),
            "sharpe": round(sharpe, 2),
            "final_equity_usd": round(equity[-1], 2),
            "compliance": {
                "daily_loss_limit_usd": round(dloss, 2),
                "daily_profit_cap_usd": round(dprofit, 2),
                "max_drawdown_usd": round(max_dd, 2),
                "within_daily_loss_limit": max_dd <= dloss,
                "passes_prop_firm": (max_dd <= dloss) and (net <= dprofit),
            },
            "sample_trades": trades[:5],
        }


def analyze_trades(log_csv=None):
    """Analyze trading state from bot.py's live status."""
    try:
        bot = _import_trading_module("bot")
        status = bot.read_status()
        return {
            "ok": True,
            "running": status.get("running", False),
            "in_trade": status.get("in_trade", False),
            "trades_today": status.get("trades_today", 0),
            "total_pnl": status.get("total_pnl", 0),
            "daily_pnl": status.get("daily_pnl", 0),
            "profitable_days": status.get("profitable_days", 0),
            "wins": status.get("wins", 0),
            "losses": status.get("losses", 0),
            "last_trade_result": status.get("last_trade_result"),
            "peak_balance": status.get("peak_balance", 5000),
            "drawdown_pct": round(
                (5000 + status.get("total_pnl", 0) - status.get("peak_balance", 5000)) /
                max(1, status.get("peak_balance", 5000)) * 100, 2),
            "note": "Live bot status — consistent with bot.py state.",
        }
    except Exception as e:
        return {"ok": False, "error": str(e)}


def trading_status():
    """Live bot + MT5 snapshot."""
    from . import tools
    try:
        bot = _import_trading_module("bot")
        tconfig = _import_trading_module("config")
        s = bot.read_status()
        result = {
            "ok": True,
            "live_mode": s.get("running", False),
            "symbol": "EURUSD",
            "bot_running": s.get("running", False),
            "in_trade": s.get("in_trade", False),
            "daily_pnl_usd": round(s.get("daily_pnl", 0), 2),
            "total_pnl_usd": round(s.get("total_pnl", 0), 2),
            "trades_today": s.get("trades_today", 0),
            "profitable_days": s.get("profitable_days", 0),
            "wins": s.get("wins", 0),
            "losses": s.get("losses", 0),
            "last_trade": s.get("last_trade_result"),
            "risk_usd": tconfig.RISK_USD(),
            "reward_usd": tconfig.REWARD_USD(),
            "sl_pips": tconfig.SL_PIPS(),
            "tp_pips": tconfig.TP_PIPS(),
            "strategy": "London ORB Retest",
            "account_balance": tconfig.ACCOUNT_BALANCE,
            "daily_loss_limit": tconfig.DAILY_LOSS_LIMIT(),
            "max_profit_cap": tconfig.MAX_PROFIT,
            "profitable_days_required": tconfig.MIN_TRADING_DAYS,
        }
        return result
    except Exception as e:
        return {"ok": False, "error": str(e)}


def _load_config():
    cfg = {
        "ACCOUNT_BALANCE": 5000.0,
        "DAILY_LOSS_LIMIT_PCT": 0.03,
        "DAILY_PROFIT_CAP": 30.0,
        "RISK_USD": 25.0,
        "LIVE_MODE": False,
        "SYMBOL": "AUDCAD",
    }
    try:
        import importlib.util
        spec = importlib.util.spec_from_file_location(
            "trading_config", os.path.join(_TRADING_DIR, "config.py"))
        mod = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(mod)
        for k in cfg:
            if hasattr(mod, k):
                cfg[k] = getattr(mod, k)
    except Exception:
        pass
    return cfg


def _bollinger_rsi_signals(df, bb_period=20, bb_std=2, rsi_period=14):
    import pandas as pd
    import numpy as np
    closes = df["close"].astype(float)
    ma = closes.rolling(bb_period).mean()
    std = closes.rolling(bb_period).std()
    upper = ma + bb_std * std
    lower = ma - bb_std * std
    delta = closes.diff()
    gain = (delta.where(delta > 0, 0)).rolling(rsi_period).mean()
    loss = (-delta.where(delta < 0, 0)).rolling(rsi_period).mean()
    rs = gain / loss
    rsi = 100 - (100 / (1 + rs))
    signals = []
    for i in range(len(closes)):
        if pd.isna(upper.iloc[i]) or pd.isna(rsi.iloc[i]):
            continue
        if closes.iloc[i] < lower.iloc[i] and rsi.iloc[i] < 30:
            signals.append((i, "BUY"))
        elif closes.iloc[i] > upper.iloc[i] and rsi.iloc[i] > 70:
            signals.append((i, "SELL"))
    return signals, ma, lower, upper


def _fetch_data(symbol, timeframe, days):
    import MetaTrader5 as mt5
    from trading import config as _tc
    if not mt5.initialize(login=_tc.MT5_LOGIN, password=_tc.MT5_PASSWORD, server=_tc.MT5_SERVER):
        raise RuntimeError("MT5 not initialized")
    tf_map = {
        "M15": mt5.TIMEFRAME_M15, "H1": mt5.TIMEFRAME_H1,
        "M5": mt5.TIMEFRAME_M5, "H4": mt5.TIMEFRAME_H4, "D1": mt5.TIMEFRAME_D1,
    }
    tf = tf_map.get(timeframe, mt5.TIMEFRAME_M15)
    n = int(days * 24 * 60 / 15) if timeframe == "M15" else days * 24
    rates = mt5.copy_rates_from_pos(symbol, tf, 0, min(n, 50000))
    if rates is None:
        raise RuntimeError("MT5 copy_rates failed")
    import pandas as pd
    df = pd.DataFrame(rates)
    df["time"] = pd.to_datetime(df["time"], unit="s")
    return df
