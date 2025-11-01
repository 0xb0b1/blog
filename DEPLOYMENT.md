# Deployment Guide

## Option 1: Fly.io (Recommended)

Fly.io offers a generous free tier and is perfect for Go applications.

### Prerequisites
```bash
# Install flyctl
curl -L https://fly.io/install.sh | sh

# Sign up/login
fly auth signup
# or
fly auth login
```

### Deploy
```bash
# Launch app (first time only)
fly launch --no-deploy

# Deploy
fly deploy

# Open in browser
fly open
```

### View Logs
```bash
fly logs
```

### Scale (if needed)
```bash
fly scale count 2  # Run 2 instances
fly scale vm shared-cpu-1x --memory 512  # More resources
```

---

## Option 2: Railway

Railway offers automatic deployments from GitHub.

### Steps
1. Go to [railway.app](https://railway.app)
2. Sign in with GitHub
3. Click "New Project" → "Deploy from GitHub repo"
4. Select your blog repository
5. Railway auto-detects Dockerfile and deploys
6. Set environment variable: `PORT=8080`
7. Your blog will be live at `*.railway.app`

---

## Option 3: Render

Free tier with automatic SSL.

### Steps
1. Go to [render.com](https://render.com)
2. Sign in with GitHub
3. Click "New +" → "Web Service"
4. Connect your repository
5. Settings:
   - **Build Command**: `go build -o main .`
   - **Start Command**: `./main`
   - **Environment**: `PORT=8080`
6. Deploy

---

## Option 4: DigitalOcean App Platform

Simple deployment with $5/month pricing.

### Steps
1. Go to [cloud.digitalocean.com](https://cloud.digitalocean.com)
2. Create → Apps → Deploy your GitHub repository
3. DigitalOcean auto-detects Go
4. Configure:
   - **HTTP Port**: 8080
   - **Instance Size**: Basic ($5/mo)
5. Deploy

---

## Option 5: VPS (Manual Setup)

For full control using any VPS provider (DigitalOcean, Linode, AWS EC2, etc.)

### Setup
```bash
# SSH into your server
ssh user@your-server-ip

# Install Go
wget https://go.dev/dl/go1.25.1.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.25.1.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/bin/go/bin' >> ~/.bashrc
source ~/.bashrc

# Clone your repository
git clone https://github.com/0xb0b1/blog.git
cd blog

# Build
go build -o blog

# Create systemd service
sudo nano /etc/systemd/system/blog.service
```

Add this content:
```ini
[Unit]
Description=Paulo's Blog
After=network.target

[Service]
Type=simple
User=your-user
WorkingDirectory=/home/your-user/blog
ExecStart=/home/your-user/blog/blog
Restart=always
Environment=PORT=8080

[Install]
WantedBy=multi-user.target
```

Enable and start:
```bash
sudo systemctl enable blog
sudo systemctl start blog
sudo systemctl status blog
```

### Nginx Reverse Proxy
```bash
sudo apt install nginx

sudo nano /etc/nginx/sites-available/blog
```

Add:
```nginx
server {
    listen 80;
    server_name yourdomain.com;

    location / {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }
}
```

Enable:
```bash
sudo ln -s /etc/nginx/sites-available/blog /etc/nginx/sites-enabled/
sudo nginx -t
sudo systemctl restart nginx
```

### SSL with Let's Encrypt
```bash
sudo apt install certbot python3-certbot-nginx
sudo certbot --nginx -d yourdomain.com
```

---

## Option 6: Docker + Any Platform

Build and push to Docker Hub, then deploy anywhere.

### Build and Push
```bash
# Build
docker build -t yourusername/pv-blog:latest .

# Test locally
docker run -p 8080:8080 yourusername/pv-blog:latest

# Push to Docker Hub
docker login
docker push yourusername/pv-blog:latest
```

### Deploy on any platform that supports Docker
- AWS ECS
- Google Cloud Run
- Azure Container Instances
- Any Kubernetes cluster

---

## Recommended Choice

**For beginners**: Fly.io or Railway (easiest, free tier)
**For control**: VPS with your own domain
**For scale**: AWS ECS or Google Cloud Run

## Cost Comparison

| Platform | Free Tier | Paid (Starting) |
|----------|-----------|-----------------|
| Fly.io | 3 shared VMs | $5/mo |
| Railway | 500 hours/mo | $5/mo |
| Render | 750 hours/mo | $7/mo |
| DigitalOcean | No | $5/mo |
| VPS (any) | No | $5-10/mo |

## Custom Domain

Once deployed, you can add a custom domain:

### Fly.io
```bash
fly certs add yourdomain.com
```
Point your DNS A record to Fly.io's IP.

### Other Platforms
Follow their respective documentation for custom domains. Most support automatic SSL via Let's Encrypt.

---

## Continuous Deployment

### GitHub Actions (Fly.io)

Create `.github/workflows/deploy.yml`:

```yaml
name: Deploy to Fly.io

on:
  push:
    branches: [main]

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: superfly/flyctl-actions/setup-flyctl@master
      - run: flyctl deploy --remote-only
        env:
          FLY_API_TOKEN: ${{ secrets.FLY_API_TOKEN }}
```

Get token: `fly tokens create deploy`
Add to GitHub: Settings → Secrets → New repository secret

---

## Monitoring

### Fly.io
```bash
fly status
fly logs
fly metrics
```

### Custom Monitoring
Add these endpoints to your app for health checks:

```go
// In main.go
http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
    w.Write([]byte("OK"))
})
```

---

Need help choosing? **Start with Fly.io** - it's free, easy, and perfect for Go apps!
