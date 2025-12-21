# SubTrackr

A self-hosted subscription management application built with Go and HTMX. Track your subscriptions, visualize spending, and get renewal reminders.

![SubTrackr Dashboard](dashboard-screenshot.png)

![SubTrackr Calendar View](calendar-screenshot.png)

![SubTrackr Mobile View](mobile-screenshot.png)

## 🎨 Themes

Personalize your SubTrackr experience with 5 beautiful themes:

<table>
  <tr>
    <td align="center">
      <img src="screenshots/christmas.png" alt="Christmas Theme" width="600"/><br/>
      <b>Christmas 🎄</b><br/>
      Festive and jolly! (with snowfall animation)
    </td>
  </tr>
  <tr>
    <td align="center">
      <img src="screenshots/ocean.png" alt="Ocean Theme" width="600"/><br/>
      <b>Ocean</b><br/>
      Cool and refreshing
    </td>
  </tr>
  <tr>
    <td align="center">
      <img src="screenshots/login.png" alt="Login Page" width="600"/><br/>
      <b>Optional Authentication</b><br/>
      Secure your data with optional login support
    </td>
  </tr>
</table>

**Available themes:** Default (Light), Dark, Christmas 🎄, Midnight (Purple), Ocean (Cyan)

Themes persist across all pages and are saved per user. Change themes anytime from Settings → Appearance.

![Version](https://img.shields.io/github/v/release/bscott/subtrackr?logo=github&label=version)
![Go Version](https://img.shields.io/badge/go-%3E%3D1.21-00ADD8)
![License](https://img.shields.io/badge/license-AGPL--3.0-green)

## 🚀 Features

- 📊 **Dashboard Overview**: Real-time stats showing monthly/annual spending
- 💰 **Subscription Management**: Track all your subscriptions in one place with logos
- 📅 **Calendar View**: Visual calendar showing all subscription renewal dates with iCal export
- 📈 **Analytics**: Visualize spending by category and track savings
- 🔔 **Email Notifications**: Get reminders before subscriptions renew
- 📤 **Data Export**: Export your data as CSV, JSON, or iCal format
- 🎨 **Beautiful Themes**: 5 stunning themes including a festive Christmas theme with snowfall animation
- 🌍 **Multi-Currency Support**: Support for USD, EUR, GBP, JPY, RUB, SEK, PLN, and INR (with optional real-time conversion)
- 🐳 **Docker Ready**: Easy deployment with Docker
- 🔒 **Self-Hosted**: Your data stays on your server
- 📱 **Mobile Responsive**: Optimized mobile experience with hamburger menu navigation

## 🏗️ Tech Stack

- **Backend**: Go with Gin framework
- **Database**: SQLite (no external database needed!)
- **Frontend**: HTMX + Tailwind CSS
- **Deployment**: Docker & Docker Compose

## 🚀 Quick Start

SubTrackr is available as a multi-platform Docker image supporting both AMD64 and ARM64 architectures (including Apple Silicon).

**Note:** SubTrackr works fully out-of-the-box with no external dependencies. The Fixer.io API key is completely optional for currency conversion features.

### Option 1: Docker Compose (Recommended)

1. **Create docker-compose.yml**:

```yaml
version: '3.8'

services:
  subtrackr:
    image: ghcr.io/bscott/subtrackr:latest
    container_name: subtrackr
    ports:
      - "8080:8080"
    volumes:
      - ./data:/app/data
    environment:
      - GIN_MODE=release
      - DATABASE_PATH=/app/data/subtrackr.db
      # Optional: Enable automatic currency conversion (requires Fixer.io API key)
      # - FIXER_API_KEY=your_fixer_api_key_here
    restart: unless-stopped
```

2. **Start the container**:

```bash
docker-compose up -d
```

3. **Access SubTrackr**: Open http://localhost:8080

### Option 2: Docker Run

```bash
docker run -d \
  --name subtrackr \
  -p 8080:8080 \
  -v $(pwd)/data:/app/data \
  -e GIN_MODE=release \
  ghcr.io/bscott/subtrackr:latest

# Optional: With currency conversion enabled
docker run -d \
  --name subtrackr \
  -p 8080:8080 \
  -v $(pwd)/data:/app/data \
  -e GIN_MODE=release \
  -e FIXER_API_KEY=your_fixer_api_key_here \
  ghcr.io/bscott/subtrackr:latest
```

### Option 3: Build from Source

1. **Clone the repository**:
```bash
git clone https://github.com/bscott/subtrackr.git
cd subtrackr
```

2. **Build and run with Docker Compose**:
```bash
docker-compose up -d --build
```

## 🐳 Deployment Guides

### Portainer

1. **Stack Deployment**:
   - Go to Stacks → Add Stack
   - Name: `subtrackr`
   - Paste the docker-compose.yml content
   - Deploy the stack

2. **Environment Variables** (optional):
   ```
   PORT=8080
   DATABASE_PATH=/app/data/subtrackr.db
   GIN_MODE=release
   ```

3. **Volumes**:
   - Create a volume named `subtrackr-data`
   - Mount to `/app/data` in the container

### Proxmox LXC Container

1. **Create LXC Container**:
   ```bash
   # Create container (Ubuntu 22.04)
   pct create 200 local:vztmpl/ubuntu-22.04-standard_22.04-1_amd64.tar.gz \
     --hostname subtrackr \
     --memory 512 \
     --cores 1 \
     --net0 name=eth0,bridge=vmbr0,ip=dhcp \
     --storage local-lvm \
     --rootfs local-lvm:8
   ```

2. **Install Docker in LXC**:
   ```bash
   pct start 200
   pct enter 200
   
   # Update and install Docker
   apt update && apt upgrade -y
   curl -fsSL https://get.docker.com | sh
   ```

3. **Deploy SubTrackr**:
   ```bash
   mkdir -p /opt/subtrackr
   cd /opt/subtrackr
   
   # Create docker-compose.yml
   nano docker-compose.yml
   # Paste the docker-compose content
   
   docker-compose up -d
   ```

### Unraid

1. **Community Applications**:
   - Search for "SubTrackr" in CA
   - Configure paths and ports
   - Apply

2. **Manual Docker Template**:
   - Repository: `ghcr.io/bscott/subtrackr:latest`
   - Port: `8080:8080`
   - Path: `/app/data` → `/mnt/user/appdata/subtrackr`

### Synology NAS

1. **Using Docker Package**:
   - Open Docker package
   - Registry → Search "subtrackr"
   - Download latest image
   - Create container with port 8080 and volume mapping

2. **Using Container Manager** (DSM 7.2+):
   - Project → Create
   - Upload docker-compose.yml
   - Build and run

## 🔧 Configuration

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `PORT` | Server port | `8080` |
| `DATABASE_PATH` | SQLite database file path | `./data/subtrackr.db` |
| `GIN_MODE` | Gin framework mode (debug/release) | `debug` |
| `FIXER_API_KEY` | Fixer.io API key for currency conversion (optional) | None |

### Currency Conversion (Optional)

SubTrackr supports automatic currency conversion using Fixer.io exchange rates:

**Without API key:** (Fully functional)
- Basic multi-currency support with display symbols
- Manual currency selection per subscription
- Subscriptions displayed in their original currency
- No automatic conversion between currencies

**With Fixer.io API key:**
- Real-time exchange rates (cached for 24 hours)
- Automatic conversion between any supported currencies
- Display original amount + converted amount in your preferred currency

**Setup:**
1. Sign up for free at [Fixer.io](https://fixer.io/) (1000 requests/month)
2. Get your API key from the dashboard
3. Add `FIXER_API_KEY=your_key_here` to your environment variables
4. Restart SubTrackr - currency conversion will be automatically enabled

**Note:** The free Fixer.io plan only allows EUR as the base currency. SubTrackr automatically handles cross-rate calculations (e.g., USD→INR goes through EUR) so all currency conversions work correctly regardless of this limitation.

**Supported currencies:** USD, EUR, GBP, JPY, RUB, SEK, PLN, INR

### Email Notifications (SMTP)

Configure SMTP settings in the web interface:

1. Navigate to Settings → Email Notifications
2. Enter your SMTP details:
   - **Gmail**: smtp.gmail.com:587
   - **Outlook**: smtp-mail.outlook.com:587
   - **Custom**: Your SMTP server details
3. Test connection
4. Enable renewal reminders

### Data Persistence

**Important**: Always mount a volume to `/app/data` to persist your database!

```yaml
volumes:
  - ./data:/app/data  # Local directory
  # OR
  - subtrackr-data:/app/data  # Named volume
```

## 🔐 Security Recommendations

1. **Reverse Proxy**: Use Nginx/Traefik for HTTPS
2. **Authentication**: Add basic auth or OAuth2 proxy
3. **Network**: Don't expose port 8080 directly to internet
4. **Backups**: Regular backups of the data directory

### Nginx Reverse Proxy Example

```nginx
server {
    server_name subtrackr.yourdomain.com;
    
    location / {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

### Traefik Labels

```yaml
labels:
  - "traefik.enable=true"
  - "traefik.http.routers.subtrackr.rule=Host(`subtrackr.yourdomain.com`)"
  - "traefik.http.routers.subtrackr.entrypoints=websecure"
  - "traefik.http.routers.subtrackr.tls.certresolver=letsencrypt"
```

## 📊 API Documentation

SubTrackr provides a RESTful API for external integrations. All API endpoints require authentication using an API key.

### Authentication

Create an API key from the Settings page in the web interface. Include the API key in your requests using one of these methods:

```bash
# Authorization header (recommended)
curl -H "Authorization: Bearer sk_your_api_key_here" https://your-domain.com/api/v1/subscriptions

# X-API-Key header
curl -H "X-API-Key: sk_your_api_key_here" https://your-domain.com/api/v1/subscriptions
```

### API Endpoints

#### Subscriptions

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/subscriptions` | List all subscriptions |
| POST | `/api/v1/subscriptions` | Create a new subscription |
| GET | `/api/v1/subscriptions/:id` | Get subscription details |
| PUT | `/api/v1/subscriptions/:id` | Update subscription |
| DELETE | `/api/v1/subscriptions/:id` | Delete subscription |

#### Statistics & Export

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/stats` | Get subscription statistics |
| GET | `/api/v1/export/csv` | Export subscriptions as CSV |
| GET | `/api/v1/export/json` | Export subscriptions as JSON |

### Example Requests

#### List Subscriptions
```bash
curl -H "Authorization: Bearer sk_your_api_key_here" \
  https://your-domain.com/api/v1/subscriptions
```

#### Create Subscription
```bash
curl -X POST \
  -H "Authorization: Bearer sk_your_api_key_here" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Netflix",
    "cost": 15.99,
    "schedule": "Monthly",
    "status": "Active",
    "category": "Entertainment"
  }' \
  https://your-domain.com/api/v1/subscriptions
```

#### Update Subscription
```bash
curl -X PUT \
  -H "Authorization: Bearer sk_your_api_key_here" \
  -H "Content-Type: application/json" \
  -d '{
    "cost": 17.99,
    "status": "Active"
  }' \
  https://your-domain.com/api/v1/subscriptions/123
```

#### Get Statistics
```bash
curl -H "Authorization: Bearer sk_your_api_key_here" \
  https://your-domain.com/api/v1/stats
```

Response:
```json
{
  "total_count": 15,
  "active_count": 12,
  "total_cost": 245.67,
  "categories": {
    "Entertainment": 45.99,
    "Productivity": 89.00,
    "Storage": 29.99
  }
}
```

## 🛠️ Development

### Prerequisites

- Go 1.21+
- Docker (optional)

### Local Development

```bash
# Install dependencies
go mod download

# Run development server
go run cmd/server/main.go

# Build binary
go build -o subtrackr cmd/server/main.go
```

### Building Docker Image

```bash
# Build for current platform
docker build -t subtrackr:latest .

# Build multi-platform
docker buildx build --platform linux/amd64,linux/arm64 \
  -t subtrackr:latest --push .
```

## 🤝 Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/AmazingFeature`)
3. Commit your changes (`git commit -m 'Add some AmazingFeature'`)
4. Push to the branch (`git push origin feature/AmazingFeature`)
5. Open a Pull Request

## 📝 License

This project is licensed under the GNU Affero General Public License v3.0 (AGPL-3.0) - see the [LICENSE](LICENSE) file for details.

## 🙏 Acknowledgments

- Built with [Gin](https://gin-gonic.com/) web framework
- UI powered by [HTMX](https://htmx.org/) and [Tailwind CSS](https://tailwindcss.com/)
- Icons from [Heroicons](https://heroicons.com/)

## ⚠️ Known Limitations

- **Price History**: SubTrackr currently tracks only the current price per subscription. If a subscription changes price over time, annual spend calculations will be based on the current price multiplied by the billing cycle, which may not reflect actual historical spending. For accurate historical tracking, consider manually updating subscription costs when prices change or keeping external records.

## 📞 Support

- 🐛 Issues: [GitHub Issues](https://github.com/bscott/subtrackr/issues)
- 💬 Discussions: [GitHub Discussions](https://github.com/bscott/subtrackr/discussions)

---

Made with ❤️ by me and Vibing 