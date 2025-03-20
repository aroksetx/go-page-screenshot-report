# Screenshot Tool

A robust Go application that automatically captures and analyzes screenshots of web pages.

## Features

- Captures full-page screenshots of entire web page content
- Generates viewport-limited screenshots divided into sections
- Supports concurrent processing of multiple URLs
- Customizable viewport dimensions
- Configurable page loading delay times
- Organized screenshot storage with consistent naming
- **Automatic Chrome fallback**: Uses local Chrome if available, otherwise tries Docker

## Requirements

- Go 1.18 or later
- One of the following:
  - Chrome/Chromium browser installed locally
  - Docker installed (for automatic Docker Chrome fallback)
  - Browserless.io account (optional)

### Chrome Selection Logic

The tool automatically selects Chrome in this priority order:

1. If `BROWSERLESS_TOKEN` environment variable is set, use browserless.io
2. If Chrome is installed locally, use the local Chrome executable
3. If Docker is installed, automatically start a Chrome container
4. Fall back to default Chrome settings (which may fail if Chrome isn't installed)

No configuration is required for the automatic fallback behavior - the tool will try to find the best available option.

### Local Chrome Installation

The application will attempt to automatically locate Chrome in common installation locations:

- **macOS**: 
  - `/Applications/Google Chrome.app/Contents/MacOS/Google Chrome`
  - `/Applications/Google Chrome Canary.app/Contents/MacOS/Google Chrome Canary`
  - `/Applications/Chromium.app/Contents/MacOS/Chromium`

- **Windows**:
  - `C:\Program Files\Google\Chrome\Application\chrome.exe`
  - `C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`
  - `%LOCALAPPDATA%\Google\Chrome\Application\chrome.exe`

- **Linux**:
  - `/usr/bin/google-chrome`
  - `/usr/bin/chromium`
  - `/usr/bin/chromium-browser`
  - `/snap/bin/chromium`

If your Chrome installation is in a non-standard location, you can set the `CHROME_PATH` environment variable:

```bash
export CHROME_PATH=/path/to/your/chrome
```

#### 2. Serverless Chrome (Browserless.io)

For environments where installing Chrome is not feasible (like serverless deployments), you can use browserless.io:

1. Sign up for a [browserless.io](https://browserless.io) account
2. Get your API token
3. Set the environment variable:

```bash
export BROWSERLESS_TOKEN=your-token-here
```

This will connect to browserless.io's Chrome-as-a-service instead of requiring a local installation.

#### 3. Docker Chrome

You can also run Chrome in a Docker container:

```bash
docker run -d -p 9222:9222 browserless/chrome
```

Then use:

```bash
export CHROME_PATH=http://localhost:9222
```

## Installation

1. Clone the repository:
```bash
git clone https://github.com/yourusername/screenshot-tool.git
cd screenshot-tool
```

2. Install dependencies:
```bash
go mod tidy
```

## Usage

1. Configure the URLs and settings in `config.json`:
```json
{
  "urls": [
    {
      "name": "example-homepage",
      "url": "https://example.com",
      "viewports": [
        {
          "width": 1920,
          "height": 1080
        },
        {
          "width": 768,
          "height": 1024
        }
      ],
      "delay": 1000,
      "cookies": [
        {
          "name": "location",
          "value": "west-coast",
          "path": "/",
          "secure": false,
          "httpOnly": false
        }
      ],
      "localStorage": {
        "preferredLocation": "west-coast",
        "userSettings": "{\"theme\":\"dark\"}"
      }
    }
  ],
  "urlList": ["https://github.com", "https://google.com"],
  "defaultDelay": 2000,
  "defaultViewports": [
    {
      "width": 1920,
      "height": 1080
    }
  ],
  "defaultCookies": [
    {
      "name": "session",
      "value": "test-session",
      "path": "/",
      "secure": false,
      "httpOnly": false
    }
  ],
  "defaultLocalStorage": {
    "theme": "light",
    "language": "en"
  },
  "outputDir": "./screenshots",
  "fileFormat": "png",
  "quality": 80,
  "concurrency": 2
}
```

2. Run the tool:
```bash
go run main.go
```

Or with a custom configuration file:
```bash
go run main.go -config=custom-config.json
```

3. Build the tool:
```bash
go build -o screenshot-tool
```

## Configuration Options

| Option | Description |
|--------|-------------|
| `urls` | Array of URL objects to process |
| `urlList` | Simple array of URLs to process (uses defaults) |
| `defaultViewports` | Array of default viewport dimensions |
| `defaultDelay` | Default page load delay in milliseconds |
| `defaultCookies` | Default cookies to set for all URLs |
| `defaultLocalStorage` | Default localStorage values to set for all URLs |
| `outputDir` | Directory to save screenshots |
| `fileFormat` | Image format (png or jpeg) |
| `quality` | Image quality (1-100) |
| `concurrency` | Number of URLs to process simultaneously |

### URL Object Options

| Option | Description |
|--------|-------------|
| `name` | Identifier for the URL (used in filenames) |
| `url` | URL to capture |
| `viewports` | Array of custom viewport dimensions (optional) |
| `delay` | Page load delay in milliseconds (optional) |
| `cookies` | Array of cookies to set before capturing (optional) |
| `localStorage` | Object of localStorage key-value pairs to set (optional) |

### Cookie Object Options

| Option | Description |
|--------|-------------|
| `name` | Cookie name |
| `value` | Cookie value |
| `domain` | Cookie domain (optional, defaults to URL domain) |
| `path` | Cookie path (optional, defaults to "/") |
| `secure` | Whether cookie is secure (optional) |
| `httpOnly` | Whether cookie is HTTP only (optional) |

## Testing Different Server Locations

You can use the cookie and localStorage features to test websites with different server locations:

```bash
# Run with west coast configuration
go run main.go -config=config.json

# Run with east coast configuration
go run main.go -config=config-east.json
```

## Examples

### Setting Cookies and localStorage

```json
{
  "urls": [
    {
      "name": "website-west-coast",
      "url": "https://example.com",
      "cookies": [
        {
          "name": "location",
          "value": "west-coast"
        }
      ],
      "localStorage": {
        "region": "west"
      }
    }
  ]
}
```

## Output

Screenshots are saved in the specified output directory with the following structure:

```
/outputDir
  /{url-name}/
    /{timestamp}-full.{format}        # Full page screenshot
    /{timestamp}-viewport-1.{format}  # First viewport section
    /{timestamp}-viewport-2.{format}  # Second viewport section
    ...
```

## License

MIT 