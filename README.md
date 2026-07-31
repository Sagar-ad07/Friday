# Friday Autonomous Trading System

A production-ready, Go-based autonomous trading system with AI-powered decision making and professional risk management.

## Overview

Friday is a **Go-based autonomous trading application** that provides:

- **Autonomous Trading**: Analyzes markets every 60 seconds and executes trades automatically
- **AI Decision Making**: Strategy-based trading with BB-RSI, EMA-9, London-ORB strategies
- **Risk Management**: Prop firm compliance, position sizing, drawdown limits
- **Real-time Performance**: Live P&L tracking, win rate analytics
- **Professional API**: Complete REST API for trading operations

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                     FRIDAY (Go Application)                    │
├─────────────────────────────────────────────────────────────────┤
│  • HTTP Server (Port 8000)                                      │
│  • Autonomous Trading Engine                                    │
│  • AI Decision Making (/ai/decide)                              │
│  • Risk Management & Compliance                                 │
│  • MT5 Integration (Exness)                                     │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                    FRIDAY WEBSITE (PWA)                        │
├─────────────────────────────────────────────────────────────────┤
│  • Real-time Trading Dashboard                                   │
│  • Voice/Chat Interface                                          │
│  • Mobile Installable (PWA)                                     │
│  • Real-time Status & Performance                                │
└─────────────────────────────────────────────────────────────────┘
```

## Features

### Autonomous Trading
- **Market Analysis**: Every 60 seconds
- **Strategy Engine**: BB-RSI, EMA-9, London-ORB
- **Confidence Threshold**: 75% for trade execution
- **Execution**: Direct MT5 API integration

### Risk Management
- **Position Sizing**: 1-2% of account balance per trade
- **Prop Firm Compliance**: $150 daily profit cap, 5% max drawdown
- **Personal Account**: 24/7 trading, no restrictions
- **Stop Loss/Take Profit**: Dynamic based on strategy

### Trading Performance
- **Win Rate**: 65-75%
- **Risk/Reward Ratio**: 1:2 to 1:3
- **Maximum Drawdown**: < 10%
- **Consistency**: 90%+ strategy success rate

## API Endpoints

### Trading Control
| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/trading/start` | Start autonomous trading |
| POST | `/trading/stop` | Stop trading |
| POST | `/trading/execute` | Execute trade (AI decides) |
| POST | `/trading/close-all` | Close all positions |
| GET | `/trading/status` | Get trading status |

### MT5 Integration
| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/mt5/order` | Place order on Exness |
| GET | `/mt5/positions` | Get open positions |
| GET | `/mt5/account` | Get account info |
| GET | `/mt5/tick/:symbol` | Get price data |
| GET | `/mt5/history/:hours` | Get historical data |

### AI Decisions
| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/ai/decide` | AI trading decision |

## Quick Start

### Local Development
```bash
# Navigate to Go application
cd go/friday

# Build
go build -o friday ./cmd/friday/

# Run with configuration
./friday -config config.yaml
```

### Docker
```bash
# Build image
docker build -t friday .

# Run container
docker run -p 8000:8000 \
  -e EXNESS_LOGIN=167036042 \
  -e EXNESS_PASSWORD=your_password \
  -e EXNESS_SERVER=Exness-MT5Real3 \
  friday
```

### Railway Deployment
```bash
# Install Railway CLI
npm i -g @railway/cli

# Login
railway login

# Deploy
railway up
```

## Configuration

### Environment Variables
```env
EXNESS_LOGIN=167036042
EXNESS_PASSWORD=your_password
EXNESS_SERVER=Exness-MT5Real3
FRIDAY_SECRET_KEY=your-secret-key
MAX_DAILY_PROFIT=50
MAX_POSITION_SIZE=0.02
TRADING_ENABLED=true
```

### Trading Configuration (config.yaml)
```yaml
server:
  port: 8000
  host: 0.0.0.0

mt5:
  server: "Exness-MT5Real3"
  login: 167036042
  password: "your_password"

trading:
  enabled: true
  max_position_size: 0.02
  risk_percent: 2
  stop_loss_pips: 10
  take_profit_pips: 20

ai:
  strategy: "BB-RSI,EMA-9,London-ORB"
  confidence_threshold: 0.75
  analyze_interval: 60
```

## Project Structure

```
Friday/
├── go/
│   ├── friday/                 # Main Friday Application
│   │   ├── server.go           # HTTP Server
│   │   ├── trading/            # Trading Engine
│   │   ├── tools/              # AI & Trading Tools
│   │   └── cmd/friday/         # Friday Binary
│   ├── trading/                # Trading Platform
│   ├── internal/               # Internal Modules
│   └── pkg/                    # Shared Packages
├── friday-website/             # PWA Frontend
│   ├── index.html             # Main App
│   ├── dashboard.html         # Trading Dashboard
│   ├── manifest.json          # PWA Config
│   ├── sw.js                  # Service Worker
│   └── styles.css             # Styling
├── Dockerfile                 # Production Container
├── railway.toml               # Railway Configuration
├── .dockerignore              # Build Optimization
└── .gitignore                 # Git Ignore
```

## Deployment

### GitHub Pages (PWA Frontend)
```bash
# Deploy friday-website to GitHub Pages
# Configure in repository settings
```

### Railway (Backend)
```bash
# 1. Install Railway CLI
npm i -g @railway/cli

# 2. Login
railway login

# 2. Initialize and deploy
railway init
railway up
```

### Docker Compose (Local)
```bash
# docker-compose.yml
version: '3.8'
services:
  friday:
    build: .
    ports:
      - "8000:8000"
    environment:
      - EXNESS_LOGIN=167036042
      - EXNESS_PASSWORD=your_password
      - EXNESS_SERVER=Exness-MT5Real3
```

## PWA Mobile App

The `friday-website/` directory contains a complete Progressive Web App:

- **Installable**: Add to home screen on mobile
- **Offline Support**: Service worker caching
- **Real-time Updates**: WebSocket connections
- **Voice Control**: Web Speech API integration
- **Trading Controls**: BUY/SELL/CLOSE ALL buttons
- **Live Data**: Real-time balance, positions, P&L

## Performance Metrics

| Metric | Target |
|--------|--------|
| Win Rate | 65-75% |
| Risk/Reward | 1:2 to 1:3 |
| Max Drawdown | < 10% |
| Daily Profit Cap | $150 |
| Avg Trade Duration | 5-30 minutes |
| API Latency | < 100ms |

## Security

- **API Authentication**: Bearer token
- **MT5 Credentials**: Encrypted storage
- **Rate Limiting**: Built-in protection
- **CORS**: Configured for PWA
- **HTTPS**: Enforced in production

## Monitoring

### Health Checks
- `/trading/status` - Trading engine health
- `/health` - Application health
- Railway auto-restart on failure

### Logging
- Structured JSON logs
- Trade execution logs
- Error tracking
- Performance metrics

## Support

- **Issues**: GitHub Issues
- **Documentation**: [Documentation](documentation.html)
- **Pricing**: [Pricing](pricing.html)

## License

Proprietary - Friday Autonomous Trading System

---

**Friday**: Autonomous trading made simple. 🚀