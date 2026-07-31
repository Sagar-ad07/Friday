# Railway Deployment Guide for Friday AI

## Prerequisites
- Railway account (active subscription)
- Railway CLI installed (`npm install -g @railway/cli`)
- MT5 terminal access (required for trading)

## Quick Deploy

1. Login to Railway:
   ```
   railway login
   ```

2. Create a new project:
   ```
   railway init
   ```

3. Set environment variables in Railway dashboard:
   - `EXNESS_API_KEY` - Your Exness API key
   - `EXNESS_API_SECRET` - Your Exness API secret
   - `FRIDAY_SECRET_KEY` - Secret key for Friday AI
   - `TRADING_ENABLED` - Set to `true`
   - `MAX_DAILY_PROFIT` - Daily profit cap (e.g., `50`)
   - `MAX_POSITION_SIZE` - Max lot size (e.g., `0.05`)

4. Deploy:
   ```
   railway up
   ```

5. Get your deployed URL:
   ```
   railway domain
   ```

## Docker Build Notes

The Dockerfile uses a multi-stage build:
- Stage 1: Builds the Go binary with CGO enabled for MT5 support
- Stage 2: Minimal Alpine image running the binary

### MT5 Dependency
The `go-mt5` library requires MetaTrader 5 terminal libraries. For Railway deployment:
- Option A: Use a custom Docker image with MT5 pre-installed
- Option B: Use Railway's persistent storage to mount MT5 libraries
- Option C: Deploy to a VPS with MT5 pre-installed instead

## Environment Variables Reference

| Variable | Description | Required |
|----------|-------------|----------|
| `FRIDAY_SECRET_KEY` | API secret key | Yes |
| `EXNESS_API_KEY` | Exness API key | Yes |
| `EXNESS_API_SECRET` | Exness API secret | Yes |
| `TRADING_ENABLED` | Enable trading | No (default: false) |
| `MAX_DAILY_PROFIT` | Daily profit cap in USD | No (default: 50) |
| `MAX_POSITION_SIZE` | Max lot size | No (default: 0.05) |
| `LOG_LEVEL` | Log level (debug/info/warn/error) | No (default: info) |
| `PORT` | Server port | No (default: 8000) |

## Health Check

Railway monitors `/trading/status` endpoint. The service is healthy when it returns `{"running": true/false}`.

## Troubleshooting

1. **Build fails**: Check that `go.mod` and `go.sum` are in the root directory
2. **MT5 not connected**: Verify MT5 terminal is running and accessible
3. **Port already in use**: Set `PORT` environment variable
4. **Crash loops**: Check logs with `railway logs`