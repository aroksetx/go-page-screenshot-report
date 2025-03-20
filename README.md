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
- **Cookie/localStorage management**: Set and track cookies and localStorage values
- **CSV cookie logging**: Saves all cookie data in CSV format for easy analysis

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
| `cookieProfiles` | Named sets of cookies and localStorage values |
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
| `cookieProfileId` | ID of a cookie profile to apply (optional) |

### Cookie Object Options

| Option | Description |
|--------|-------------|
| `name` | Cookie name |
| `value` | Cookie value |
| `domain` | Cookie domain (optional, defaults to URL domain) |
| `path` | Cookie path (optional, defaults to "/") |
| `secure` | Whether cookie is secure (optional) |
| `httpOnly` | Whether cookie is HTTP only (optional) |

### Cookie Profiles

Cookie profiles allow you to define named sets of cookies and localStorage values that can be reused across multiple URLs. This is especially useful for testing the same site with different regional or user settings.

Benefits of cookie profiles:
- **Reusability**: Define a set of cookies once, use it for multiple URLs
- **Maintainability**: Update cookies in one place
- **Organization**: Group related cookies/localStorage together
- **A/B Testing**: Easily switch between different site configurations

### Priority Order for Cookies

The tool applies cookies in this priority order:

1. URL-specific cookies (highest priority)
2. Cookie profile cookies (if the URL has a `cookieProfileId` and no URL-specific cookies)
3. Default cookies (lowest priority, applied if no URL-specific cookies or profile)

### Cookie Logging

The tool creates log files for cookies in two formats:

#### Text Log Format
For human-readable analysis, the tool creates a text log file for each URL:
- Shows cookies before and after your custom cookies are set
- Records viewport size and screenshot type for each entry
- Lists cookies that will be applied in the "before" stage
- Shows complete details for all cookies

#### CSV Log Format
For data analysis and processing, the tool also saves cookies in CSV format:
- Contains all cookie parameters in a structured format
- Includes metadata like URL, timestamp, viewport size, and screenshot stage
- Makes it easy to analyze cookies across different URLs and stages
- Can be imported into spreadsheets or data analysis tools

Log files are saved at:
- Text logs: `./screenshots/{url-name}/{url-name}-cookies.log`
- CSV logs: `./screenshots/{url-name}/{url-name}-cookies.csv`

## Command Line Options

Run the tool with various options:

```bash
# Basic usage with configuration file
./screenshot-tool

# Use a specific configuration file
./screenshot-tool -config=custom-config.json

# Test a specific URL (uses default viewports from config or 1280x800 if none defined)
./screenshot-tool -url="https://example.com"

# Test multiple URLs
./screenshot-tool -urls="https://example.com,https://google.com"

# Specify a custom name and delay for a URL
./screenshot-tool -url="https://example.com" -name="custom-name" -delay=2000
```

**Note**: When using the `-url` or `-urls` flags, the tool will use the default viewports specified in your configuration file. If no default viewports are configured, a standard 1280x800 viewport will be used as a fallback.

## Command Line Examples

Run with different cookie configurations:

```bash
# Run with west coast configuration
./screenshot-tool -config=config-cookie-profiles.json

# To test different specific URLs only
./screenshot-tool -config=config-cookie-profiles.json -url="https://example.com"

# To test with multiple specific URLs
./screenshot-tool -config=config-cookie-profiles.json -urls="https://example.com,https://google.com"
```

## Output

Screenshots and logs are saved in the specified output directory with the following structure:

```
/outputDir
  /{url-name}/
    /{timestamp}-full-{width}x{height}.{format}     # Full page screenshot
    /{timestamp}-viewport-{width}x{height}-1.{format}  # First viewport section
    /{timestamp}-viewport-{width}x{height}-2.{format}  # Second viewport section
    ...
    /{url-name}-cookies.log                # Cookie text log
    /{url-name}-cookies.csv                # Cookie CSV log
```

## License

MIT 