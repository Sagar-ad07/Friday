# Friday AI Website - Free Deployment Guide

This website is 100% free to deploy using GitHub Pages or Netlify.

## Option 1: GitHub Pages (Recommended - Free)

### Prerequisites
- GitHub account (free)
- Git installed on your machine

### Steps

1. **Create a new GitHub repository**
   - Go to https://github.com/new
   - Name it: `friday-ai` (or any name you like)
   - Set to Public
   - Initialize with README

2. **Clone the repo**
   ```bash
   git clone https://github.com/YOUR_USERNAME/friday-ai.git
   cd friday-ai
   ```

3. **Copy website files**
   ```bash
   cp -r D:\Friday\ Prototype\friday-website\* .
   ```

4. **Commit and push**
   ```bash
   git add .
   git commit -m "Deploy Friday AI website"
   git push origin main
   ```

5. **Enable GitHub Pages**
   - Go to Settings → Pages
   - Source: Deploy from a branch
   - Branch: main
   - Save

6. **Your site is live at**
   `https://YOUR_USERNAME.github.io/friday-ai/`

## Option 2: Netlify (Also Free)

### Steps

1. **Go to https://app.netlify.com**
   - Sign up with GitHub account (free)

2. **Drag and drop**
   - Drag the `friday-website` folder onto the Netlify deploy area

3. **Your site is live instantly**
   - Netlify gives you a free URL like `friday-ai.netlify.app`

4. **Custom domain** (optional)
   - Buy a domain from Namecheap, Cloudflare, etc.
   - Add it in Netlify Site Settings → Domain Management

## What's Included

- **index.html** - Hero section, features, tools showcase, pricing, testimonials, CTA
- **documentation.html** - Complete API documentation with quick start guide
- **pricing.html** - Pricing comparison table and feature highlights
- **styles.css** - Full responsive styling with animations
- **script.js** - Full interactivity (smooth scroll, pricing toggle, forms, animations)

## Website Features

- Fully responsive (mobile, tablet, desktop)
- Dark theme with gradient accents
- Smooth scroll and hover animations
- Interactive pricing toggle
- Contact form
- API documentation page
- 77 tools showcase
- Live status indicators

## Next Steps

1. Deploy using GitHub Pages or Netlify
2. Buy a custom domain (optional)
3. Add analytics (Google Analytics, Plausible)
4. Connect to actual Friday server for live stats
5. Add real trading data to the dashboard

## Cost

- GitHub Pages: FREE
- Netlify: FREE
- Custom domain: ~$10-15/year (optional)
- Total: $0 for full deployment