package screenshot

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"screenshot-tool/config"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/storage"
	"github.com/chromedp/chromedp"
)

// findChromeExecutable attempts to locate the Chrome executable on the system
func findChromeExecutable() (string, error) {
	// Check for environment variable first
	if envPath := os.Getenv("CHROME_PATH"); envPath != "" {
		if _, err := os.Stat(envPath); err == nil {
			return envPath, nil
		}
	}

	// Common locations based on OS
	switch runtime.GOOS {
	case "darwin":
		paths := []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Google Chrome Canary.app/Contents/MacOS/Google Chrome Canary",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		}
		for _, path := range paths {
			if _, err := os.Stat(path); err == nil {
				return path, nil
			}
		}
	case "windows":
		paths := []string{
			filepath.Join(os.Getenv("ProgramFiles"), "Google/Chrome/Application/chrome.exe"),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "Google/Chrome/Application/chrome.exe"),
			filepath.Join(os.Getenv("LocalAppData"), "Google/Chrome/Application/chrome.exe"),
		}
		for _, path := range paths {
			if _, err := os.Stat(path); err == nil {
				return path, nil
			}
		}
	case "linux":
		paths := []string{
			"/usr/bin/google-chrome",
			"/usr/bin/chromium",
			"/usr/bin/chromium-browser",
			"/snap/bin/chromium",
		}
		for _, path := range paths {
			if _, err := os.Stat(path); err == nil {
				return path, nil
			}
		}
	}

	// Try finding in PATH
	if path, err := exec.LookPath("google-chrome"); err == nil {
		return path, nil
	}
	if path, err := exec.LookPath("chromium"); err == nil {
		return path, nil
	}
	if path, err := exec.LookPath("chromium-browser"); err == nil {
		return path, nil
	}

	return "", fmt.Errorf("could not find Chrome executable")
}

// startDockerChrome starts a Chrome instance in Docker if not already running
func startDockerChrome() (string, error) {
	// Check if docker is installed
	if _, err := exec.LookPath("docker"); err != nil {
		return "", fmt.Errorf("docker not installed: %w", err)
	}

	// Check if chrome container is already running
	cmd := exec.Command("docker", "ps", "-q", "-f", "name=chrome", "-f", "status=running")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to check for running chrome container: %w", err)
	}

	// If container is already running, return its address
	if len(output) > 0 {
		log.Printf("Using existing Chrome container")

		// Verify the existing container responds before continuing
		if err := checkChromeResponseFromContainer(5); err != nil {
			// Container exists but doesn't respond, stop and remove it
			log.Printf("Existing Chrome container not responding, removing it: %v", err)
			stopCmd := exec.Command("docker", "rm", "-f", "chrome")
			stopCmd.Run() // Ignore errors, we'll try to recreate
		} else {
			return "http://localhost:9222", nil
		}
	}

	// Start a new chrome container with improved configuration
	log.Printf("Starting Chrome container...")
	cmd = exec.Command("docker", "run", "-d", "--rm", "--name", "chrome",
		"-p", "9222:9222", // Using standard port 9222 for chromedp/headless-shell
		"--cap-add=SYS_ADMIN",              // Add capabilities needed for Chrome
		"--shm-size=2g",                    // Increase shared memory size to 2GB
		"--memory=4g",                      // Limit container memory to 4GB
		"chromedp/headless-shell:latest",   // Use chromedp's official headless shell image
		"--disable-web-security",           // Disable web security for testing
		"--ignore-certificate-errors",      // Ignore SSL certificate errors
		"--allow-running-insecure-content", // Allow loading insecure content
		"--disable-dev-shm-usage",          // Don't use /dev/shm (prevents crashes)
		"--no-sandbox")                     // No sandbox for container environment

	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to start chrome container: %w, output: %s", err, string(output))
	}

	// Wait for container to be ready with increased timeout
	log.Printf("Waiting for Chrome container to be ready (this may take up to 20 seconds)...")

	// Check if Chrome responds within timeout
	if err := checkChromeResponseFromContainer(20); err != nil {
		// Get container logs for diagnostics
		logsCmd := exec.Command("docker", "logs", "chrome")
		logs, _ := logsCmd.CombinedOutput()

		// Stop the container since it's not working
		stopCmd := exec.Command("docker", "rm", "-f", "chrome")
		stopCmd.Run() // Ignore errors

		return "", fmt.Errorf("chrome container started but not responding: %v\nContainer logs: %s",
			err, string(logs))
	}

	log.Printf("Chrome container is ready")
	return "http://localhost:9222", nil
}

// checkChromeResponseFromContainer checks if Chrome is responding in the container
// with the specified timeout in seconds
func checkChromeResponseFromContainer(timeoutSeconds int) error {
	// Try multiple times with increasing delay
	maxRetries := timeoutSeconds
	baseDelay := 1 * time.Second

	for i := 0; i < maxRetries; i++ {
		// Try standard Chrome endpoint first
		cmd := exec.Command("curl", "-s", "--max-time", "2", "http://localhost:9222/json/version")
		output, err := cmd.CombinedOutput()

		if err == nil && strings.Contains(string(output), "webSocketDebuggerUrl") {
			// Chrome is responding properly
			return nil
		}

		// Try browserless endpoint which might be different
		cmd = exec.Command("curl", "-s", "--max-time", "2", "http://localhost:9222/json")
		output, err = cmd.CombinedOutput()

		if err == nil && len(output) > 0 && (strings.Contains(string(output), "webSocketDebuggerUrl") ||
			strings.Contains(string(output), "browserless")) {
			// Browserless is responding
			return nil
		}

		// Increase delay slightly as we retry
		delay := baseDelay + time.Duration(i*150)*time.Millisecond
		log.Printf("Waiting for Chrome to be ready in container (attempt %d/%d)...", i+1, maxRetries)
		time.Sleep(delay)
	}

	return fmt.Errorf("timeout after %d seconds", timeoutSeconds)
}

// Screenshoter handles the screenshot capturing logic
type Screenshoter struct {
	Config *config.Config
}

// NewScreenshoter creates a new Screenshoter
func NewScreenshoter(cfg *config.Config) *Screenshoter {
	return &Screenshoter{
		Config: cfg,
	}
}

// setCookiesAndLocalStorage sets cookies and localStorage items for a URL and refreshes the page
func (s *Screenshoter) setCookiesAndLocalStorage(ctx context.Context, urlConfig config.URLConfig, viewport config.Viewport, urlDir, stage string, screenshotType string) chromedp.ActionFunc {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		// Check if these are defaultCookies (applied from config)
		defaultCookiesApplied := false
		if len(urlConfig.Cookies) > 0 && (stage == "before" || stage == "before-viewport") {
			defaultCookiesApplied = true
			log.Printf("Setting cookies from DefaultCookies configuration")
		}

		// Flag to track if any cookie or localStorage values were changed
		needsRefresh := false

		// Add cookies if specified
		if len(urlConfig.Cookies) > 0 {
			log.Printf("Setting %d cookies for %s", len(urlConfig.Cookies), urlConfig.Name)

			// Get existing cookies first
			existingCookies, err := storage.GetCookies().Do(ctx)
			if err != nil {
				log.Printf("ERROR: Failed to get existing cookies: %v", err)
				return err
			}

			// Create a map of existing cookies for quick lookup
			existingCookieMap := make(map[string]string)
			for _, cookie := range existingCookies {
				key := cookie.Name + cookie.Path + cookie.Domain
				existingCookieMap[key] = cookie.Value
			}

			// Create cookie expiration (180 days)
			expr := cdp.TimeSinceEpoch(time.Now().Add(180 * 24 * time.Hour))

			for _, cookie := range urlConfig.Cookies {
				// Extract domain from URL if not specified in cookie
				domain := cookie.Domain
				if domain == "" {
					// Use the URL's domain
					domain = extractDomainFromURL(urlConfig.URL)
				}

				// Set cookie path to root if not specified
				path := cookie.Path
				if path == "" {
					path = "/"
				}

				// Check if this cookie already exists with the same value
				key := cookie.Name + path + domain
				if value, exists := existingCookieMap[key]; exists && value == cookie.Value {
					log.Printf("Cookie %s already exists with the same value, skipping", cookie.Name)
					continue
				}

				err := network.SetCookie(cookie.Name, cookie.Value).
					WithExpires(&expr).
					WithDomain(domain).
					WithPath(path).
					WithHTTPOnly(cookie.HTTPOnly).
					WithSecure(cookie.Secure).
					Do(ctx)

				if err != nil {
					return err
				}

				needsRefresh = true
			}
		}

		// Set localStorage values if specified
		if len(urlConfig.LocalStorage) > 0 {
			log.Printf("Setting %d localStorage items for %s", len(urlConfig.LocalStorage), urlConfig.Name)
			for _, storage := range urlConfig.LocalStorage {
				jsScript := fmt.Sprintf(`
				(function() {
					const existingValue = localStorage.getItem("%s");
					if (existingValue === "%s") {
						console.log("localStorage key %s already has the same value, skipping");
						return false;
					}
					localStorage.setItem("%s", "%s");
					return true;
				})()`,
					escapeJSString(storage.Key),
					escapeJSString(storage.Value),
					escapeJSString(storage.Key),
					escapeJSString(storage.Key),
					escapeJSString(storage.Value))

				var changed bool
				if err := chromedp.Evaluate(jsScript, &changed).Do(ctx); err != nil {
					return err
				}

				if changed {
					needsRefresh = true
				}
			}
		}

		// Only refresh if needed
		if needsRefresh || defaultCookiesApplied {
			log.Printf("Refreshing page to ensure cookies and localStorage are applied")
			if err := chromedp.Reload().Do(ctx); err != nil {
				return err
			}

			// Extra refresh for DefaultCookies to ensure they're fully applied
			if defaultCookiesApplied {
				log.Printf("Adding extra refresh to ensure DefaultCookies are fully applied")
				// Reduced sleep time
				if err := chromedp.Sleep(300 * time.Millisecond).Do(ctx); err != nil {
					return err
				}
				if err := chromedp.Reload().Do(ctx); err != nil {
					return err
				}
			}

			// Reduced wait time for page to load after refresh
			if err := chromedp.Sleep(500 * time.Millisecond).Do(ctx); err != nil {
				return err
			}
		}

		// Log cookies after setting our custom ones
		return SaveCookiesToFile(ctx, urlConfig, stage, urlDir, viewport, screenshotType).Do(ctx)
	})
}

// CaptureURL captures screenshots for a given URL with all configured viewports
func (s *Screenshoter) CaptureURL(ctx context.Context, urlConfig config.URLConfig) error {
	// Create context with timeout - increase for complex pages
	// Calculate a longer timeout based on the number of viewports and complexity
	viewportsCount := len(urlConfig.Viewports)
	// Increase timeout calculation: base time (120s) + time per viewport (60s) * number of viewports
	timeoutDuration := 120*time.Second + time.Duration(60*viewportsCount)*time.Second
	ctx, cancel := context.WithTimeout(ctx, timeoutDuration)
	defer cancel()

	log.Printf("Set timeout of %v for URL %s with %d viewports", timeoutDuration, urlConfig.Name, viewportsCount)

	// Create a unique timestamp for this capture session
	timestamp := time.Now().Format("20060102-150405")
	// Create a unique directory name using both the URL name and timestamp
	uniqueDirName := fmt.Sprintf("%s_%s", sanitizeFilename(urlConfig.Name), timestamp)

	// Create base directory for this URL
	urlDir := filepath.Join(s.Config.OutputDir, uniqueDirName)
	if err := os.MkdirAll(urlDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory for URL %s: %w", urlConfig.Name, err)
	}

	log.Printf("Created unique directory for %s: %s", urlConfig.Name, uniqueDirName)

	// Check if ViewProof is needed
	viewproofNeeded := len(s.Config.ViewProof) > 0

	// Use a WaitGroup to wait for all viewports to be processed
	var wg sync.WaitGroup
	// Create error channel for goroutines
	errChan := make(chan error, len(urlConfig.Viewports))

	// Create semaphore to limit parallel viewport processing to prevent excessive resource usage
	// This is a separate semaphore from the URL concurrency one
	viewportSem := make(chan struct{}, 3) // Process up to 3 viewports in parallel

	// Process each viewport for this URL
	for i, viewport := range urlConfig.Viewports {
		wg.Add(1)
		// Capture viewport in a goroutine
		go func(i int, viewport config.Viewport) {
			defer wg.Done()

			// Acquire semaphore
			viewportSem <- struct{}{}
			defer func() { <-viewportSem }()

			// Create subdirectory for this viewport
			viewportDirName := fmt.Sprintf("%dx%d", viewport.Width, viewport.Height)
			viewportDir := filepath.Join(urlDir, viewportDirName)
			if err := os.MkdirAll(viewportDir, 0755); err != nil {
				errChan <- fmt.Errorf("failed to create directory for viewport %s: %w", viewportDirName, err)
				return
			}

			log.Printf("Capturing screenshots for %s at viewport %dx%d", urlConfig.Name, viewport.Width, viewport.Height)

			// Standard capture for all viewports (with ViewProof for all viewports when needed)
			if err := s.captureWithViewport(ctx, urlConfig, viewport, viewportDir, true, viewproofNeeded); err != nil {
				errChan <- fmt.Errorf("failed to capture screenshots for %s at viewport %dx%d: %w",
					urlConfig.Name, viewport.Width, viewport.Height, err)
				return
			}
		}(i, viewport)
	}

	// Wait for all viewports to be processed
	wg.Wait()

	// Check if there were any errors
	select {
	case err := <-errChan:
		return err
	default:
		return nil
	}
}

// captureWithViewport captures screenshots for a specific viewport size
func (s *Screenshoter) captureWithViewport(ctx context.Context, urlConfig config.URLConfig, viewport config.Viewport, viewportDir string, captureViewports bool, withViewProof bool) error {
	// Create browser options
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.WindowSize(viewport.Width, viewport.Height),
		chromedp.DisableGPU,
		chromedp.NoSandbox,
		chromedp.Headless,
		chromedp.Flag("ignore-certificate-errors", true),
	)

	// Define context variables here
	var allocCtx context.Context
	var browserCtx context.Context
	var cancelAlloc context.CancelFunc
	var cancelBrowser context.CancelFunc

	// Determine which Chrome implementation to use based on the specified mode
	switch s.Config.ChromeMode {
	case "local":
		// Force use of local Chrome
		if execPath, err := findChromeExecutable(); err == nil {
			// Use local Chrome executable
			log.Printf("Using local Chrome executable at: %s", execPath)
			opts = append(opts, chromedp.ExecPath(execPath))

			// Create allocator context with local Chrome
			allocCtx, cancelAlloc = chromedp.NewExecAllocator(ctx, opts...)
			defer cancelAlloc()
		} else {
			return fmt.Errorf("local Chrome mode specified but Chrome executable not found: %v", err)
		}

	case "docker":
		// Force use of Docker Chrome
		log.Printf("Docker Chrome mode specified, starting or connecting to Docker Chrome...")
		if dockerURL, err := startDockerChrome(); err == nil {
			// Use Docker Chrome
			log.Printf("Using Docker Chrome at: %s", dockerURL)
			// Use standard Chrome debugging protocol with chromedp/headless-shell
			allocCtx, cancelAlloc = chromedp.NewRemoteAllocator(ctx, dockerURL)
			defer cancelAlloc()
		} else {
			return fmt.Errorf("docker Chrome mode specified but failed to start or connect to Docker Chrome: %v", err)
		}

	default: // "auto" mode - try local, then Docker, then fallback
		// Try local Chrome first
		if execPath, err := findChromeExecutable(); err == nil {
			// Use local Chrome executable
			log.Printf("Using local Chrome executable at: %s", execPath)
			opts = append(opts, chromedp.ExecPath(execPath))

			// Create allocator context with local Chrome
			allocCtx, cancelAlloc = chromedp.NewExecAllocator(ctx, opts...)
			defer cancelAlloc()
		} else {
			// Try Docker Chrome as fallback
			log.Printf("Local Chrome not found: %v", err)
			log.Printf("Attempting to use Docker Chrome...")

			if dockerURL, err := startDockerChrome(); err == nil {
				// Use Docker Chrome
				log.Printf("Using Docker Chrome at: %s", dockerURL)
				// Use standard Chrome debugging protocol with chromedp/headless-shell
				allocCtx, cancelAlloc = chromedp.NewRemoteAllocator(ctx, dockerURL)
				defer cancelAlloc()
			} else {
				// Fallback to default Chrome as last resort
				log.Printf("Docker Chrome failed: %v", err)
				log.Printf("Falling back to default Chrome settings")

				allocCtx, cancelAlloc = chromedp.NewExecAllocator(ctx, opts...)
				defer cancelAlloc()
			}
		}
	}

	// Create browser context
	browserCtx, cancelBrowser = chromedp.NewContext(allocCtx, chromedp.WithLogf(log.Printf))
	defer cancelBrowser()

	// If withViewProof is true, capture a full page screenshot with ViewProof first
	if withViewProof {
		if err := s.captureFullPageWithViewProof(browserCtx, urlConfig, viewport, viewportDir); err != nil {
			return fmt.Errorf("failed to capture full-proof screenshot: %w", err)
		}
	}

	// Capture full page screenshot
	if err := s.captureFullPageScreenshot(browserCtx, urlConfig, viewport, viewportDir); err != nil {
		return fmt.Errorf("failed to capture full page screenshot for %s at viewport %dx%d: %w",
			urlConfig.Name, viewport.Width, viewport.Height, err)
	}

	// Capture viewport screenshots if requested
	if captureViewports {
		if err := s.captureViewportScreenshots(browserCtx, urlConfig, viewport, viewportDir, true); err != nil {
			return fmt.Errorf("failed to capture viewport screenshots for %s at viewport %dx%d: %w",
				urlConfig.Name, viewport.Width, viewport.Height, err)
		}
	}

	return nil
}

// SaveCookiesToFile saves all current cookies to a log file
func SaveCookiesToFile(ctx context.Context, urlConfig config.URLConfig, stage string, urlDir string, viewport config.Viewport, screenshotType string) chromedp.ActionFunc {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		log.Printf("SaveCookiesToFile called for %s (stage: %s, type: %s)", urlConfig.Name, stage, screenshotType)

		// Get all cookies
		cookies, err := storage.GetCookies().Do(ctx)
		if err != nil {
			log.Printf("ERROR: Failed to get cookies: %v", err)
			return err
		}
		log.Printf("Retrieved %d cookies for %s", len(cookies), urlConfig.Name)

		// Create a single log file for the URL
		timestamp := time.Now().Format("2006-01-02 15:04:05.000")

		// Save text log
		if err := saveCookiesTextLog(cookies, urlConfig, stage, urlDir, viewport, screenshotType, timestamp); err != nil {
			log.Printf("ERROR: Failed to save cookies text log: %v", err)
			return err
		}
		log.Printf("Saved cookies to text log successfully")

		// Save CSV log
		if err := saveCookiesCSV(cookies, urlConfig, stage, urlDir, viewport, screenshotType, timestamp); err != nil {
			log.Printf("ERROR: Failed to save cookies CSV: %v", err)
			return err
		}
		log.Printf("Saved cookies to CSV successfully")

		log.Printf("Saved %d cookies to log files (viewport: %dx%d, type: %s, stage: %s)",
			len(cookies), viewport.Width, viewport.Height, screenshotType, stage)
		return nil
	})
}

// saveCookiesTextLog saves cookies in text format
func saveCookiesTextLog(cookies []*network.Cookie, urlConfig config.URLConfig, stage string, urlDir string, viewport config.Viewport, screenshotType, timestamp string) error {
	// Use the URL name directly from the config
	filename := fmt.Sprintf("%s-cookies.log", sanitizeFilename(urlConfig.Name))
	filepath := filepath.Join(urlDir, filename)

	// Format cookies as text
	var cookieText strings.Builder
	cookieText.WriteString(fmt.Sprintf("\n\n========== %s ==========\n", stage))
	cookieText.WriteString(fmt.Sprintf("URL: %s (%s)\n", urlConfig.Name, urlConfig.URL))
	cookieText.WriteString(fmt.Sprintf("Timestamp: %s\n", timestamp))
	cookieText.WriteString(fmt.Sprintf("Viewport: %dx%d\n", viewport.Width, viewport.Height))
	cookieText.WriteString(fmt.Sprintf("Screenshot Type: %s\n", screenshotType))
	cookieText.WriteString(fmt.Sprintf("Step: %s\n", stage))

	// Add information about configured cookies if we're in the "before" stage
	if strings.Contains(stage, "before") && len(urlConfig.Cookies) > 0 {
		cookieText.WriteString("\nConfigured cookies that will be set:\n")
		for i, cookie := range urlConfig.Cookies {
			cookieText.WriteString(fmt.Sprintf("  Config Cookie #%d: %s=%s (domain: %s, path: %s)\n",
				i+1, cookie.Name, cookie.Value,
				cookie.Domain, cookie.Path))
		}
	}

	cookieText.WriteString("\n----------------------------------------\n")
	cookieText.WriteString(fmt.Sprintf("Current cookies (%d):\n", len(cookies)))

	for i, cookie := range cookies {
		cookieText.WriteString(fmt.Sprintf("Cookie #%d:\n", i+1))
		cookieText.WriteString(fmt.Sprintf("  Name: %s\n", cookie.Name))
		cookieText.WriteString(fmt.Sprintf("  Value: %s\n", cookie.Value))
		cookieText.WriteString(fmt.Sprintf("  Domain: %s\n", cookie.Domain))
		cookieText.WriteString(fmt.Sprintf("  Path: %s\n", cookie.Path))
		cookieText.WriteString(fmt.Sprintf("  Expires: %s\n", time.Unix(int64(cookie.Expires), 0)))
		cookieText.WriteString(fmt.Sprintf("  Size: %d\n", cookie.Size))
		cookieText.WriteString(fmt.Sprintf("  HttpOnly: %t\n", cookie.HTTPOnly))
		cookieText.WriteString(fmt.Sprintf("  Secure: %t\n", cookie.Secure))
		cookieText.WriteString(fmt.Sprintf("  Session: %t\n", cookie.Session))
		cookieText.WriteString(fmt.Sprintf("  SameSite: %s\n", cookie.SameSite))
		cookieText.WriteString(fmt.Sprintf("  Priority: %s\n", cookie.Priority))
		cookieText.WriteString("----------------------------------------\n")
	}

	// Check if file exists and append to it
	var fileContent []byte
	if _, err := os.Stat(filepath); err == nil {
		// File exists, read existing content
		fileContent, err = os.ReadFile(filepath)
		if err != nil {
			return err
		}
	}

	// Append new content
	fileContent = append(fileContent, []byte(cookieText.String())...)

	// Write to file
	if err := os.WriteFile(filepath, fileContent, 0644); err != nil {
		return err
	}

	return nil
}

// saveCookiesCSV saves cookies in CSV format
func saveCookiesCSV(cookies []*network.Cookie, urlConfig config.URLConfig, stage string, urlDir string, viewport config.Viewport, screenshotType, timestamp string) error {
	// Use the URL name directly from the config
	filename := fmt.Sprintf("%s-cookies.csv", sanitizeFilename(urlConfig.Name))
	filepath := filepath.Join(urlDir, filename)

	log.Printf("Saving cookies to CSV file: %s", filepath)

	// Check if file exists and determine if we need to write headers
	writeHeader := true
	if _, err := os.Stat(filepath); err == nil {
		writeHeader = false
		log.Printf("CSV file exists, appending without headers")
	} else {
		log.Printf("CSV file does not exist, will create with headers")
	}

	// Open file for appending
	file, err := os.OpenFile(filepath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("ERROR: Failed to open CSV file: %v", err)
		return err
	}
	defer file.Close()

	// Write header if needed
	if writeHeader {
		headerLine := "Timestamp,URL,URL_Name,Stage,Screenshot_Type,Viewport,Cookie_Name,Cookie_Value,Domain,Path,Expires,Size,HttpOnly,Secure,Session,SameSite,Priority\n"
		if _, err := file.WriteString(headerLine); err != nil {
			log.Printf("ERROR: Failed to write CSV header: %v", err)
			return err
		}
		log.Printf("Wrote CSV headers")
	}

	// Write cookie records
	log.Printf("Writing %d cookies to CSV", len(cookies))
	for _, cookie := range cookies {
		// Escape fields that might contain commas
		urlValue := strings.ReplaceAll(urlConfig.URL, ",", "\\,")
		urlName := strings.ReplaceAll(urlConfig.Name, ",", "\\,")
		cookieName := strings.ReplaceAll(cookie.Name, ",", "\\,")
		cookieValue := strings.ReplaceAll(cookie.Value, ",", "\\,")
		cookieDomain := strings.ReplaceAll(cookie.Domain, ",", "\\,")
		cookiePath := strings.ReplaceAll(cookie.Path, ",", "\\,")

		// Format expiration date
		expiresStr := time.Unix(int64(cookie.Expires), 0).Format("2006-01-02 15:04:05")

		// Create CSV line
		line := fmt.Sprintf("%s,%s,%s,%s,%s,%dx%d,%s,%s,%s,%s,%s,%d,%t,%t,%t,%s,%s\n",
			timestamp,
			urlValue,
			urlName,
			stage,
			screenshotType,
			viewport.Width, viewport.Height,
			cookieName,
			cookieValue,
			cookieDomain,
			cookiePath,
			expiresStr,
			cookie.Size,
			cookie.HTTPOnly,
			cookie.Secure,
			cookie.Session,
			cookie.SameSite,
			cookie.Priority)

		if _, err := file.WriteString(line); err != nil {
			return err
		}
	}

	log.Printf("Successfully wrote cookies to CSV file")
	return nil
}

// captureFullPageWithViewProof captures a special full page screenshot with ViewProof data
func (s *Screenshoter) captureFullPageWithViewProof(ctx context.Context, urlConfig config.URLConfig, viewport config.Viewport, viewportDir string) error {
	if len(s.Config.ViewProof) == 0 {
		return nil // Skip if ViewProof is not needed
	}

	log.Printf("Capturing special full-proof screenshot with ViewProof data")

	var buf []byte
	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("%s-full-proof-%dx%d.%s", timestamp, viewport.Width, viewport.Height, s.Config.FileFormat)
	filepath := filepath.Join(viewportDir, filename)

	// Initialize viewproofData map
	viewproofData := make(map[string]string)

	// Create base actions list
	var tasks []chromedp.Action

	// First navigate to the URL
	tasks = append(tasks, chromedp.Navigate(urlConfig.URL))

	// Log cookies before setting our custom ones
	tasks = append(tasks, SaveCookiesToFile(ctx, urlConfig, "before", viewportDir, viewport, "full-proof"))

	// Extract ViewProof data
	tasks = append(tasks, chromedp.ActionFunc(func(ctx context.Context) error {
		// Extract cookie values
		cookies, err := storage.GetCookies().Do(ctx)
		if err != nil {
			log.Printf("ERROR: Failed to get cookies for viewproof: %v", err)
			return nil // Non-fatal error
		}

		// Store values of cookies in viewproof list
		for _, cookie := range cookies {
			for _, proofKey := range s.Config.ViewProof {
				if cookie.Name == proofKey {
					viewproofData[fmt.Sprintf("cookie:%s", cookie.Name)] = cookie.Value
				}
			}
		}

		// Extract localStorage values
		for _, proofKey := range s.Config.ViewProof {
			var value string
			err := chromedp.Evaluate(fmt.Sprintf(`localStorage.getItem("%s")`, escapeJSString(proofKey)), &value).Do(ctx)
			if err == nil && value != "" {
				viewproofData[fmt.Sprintf("localStorage:%s", proofKey)] = value
			}
		}

		log.Printf("Extracted %d viewproof values for full-proof screenshot", len(viewproofData))
		return nil
	}))

	// Set cookies and localStorage if specified
	if len(urlConfig.Cookies) > 0 || len(urlConfig.LocalStorage) > 0 {
		tasks = append(tasks, s.setCookiesAndLocalStorage(ctx, urlConfig, viewport, viewportDir, "after", "full-proof"))
	}

	// Add remaining actions for screenshot - reduce sleep times
	tasks = append(tasks,
		chromedp.Sleep(time.Duration(urlConfig.Delay)*time.Millisecond),

		// Scroll to bottom to load lazy content
		chromedp.Evaluate(`window.scrollTo(0, document.body.scrollHeight)`, nil),
		chromedp.Sleep(500*time.Millisecond), // Reduced from 1s to 500ms

		// Scroll back to top
		chromedp.Evaluate(`window.scrollTo(0, 0)`, nil),
		chromedp.Sleep(500*time.Millisecond), // Reduced from 1s to 500ms
	)

	// Add ViewProof block with direct DOM manipulation
	tasks = append(tasks, chromedp.ActionFunc(func(ctx context.Context) error {
		if len(viewproofData) > 0 {
			// Create a simple script that adds a div without interpolation issues
			script := `
			(function() {
				// Remove any existing viewproof elements
				var existing = document.getElementById('viewproof-proof-block');
				if (existing) {
					existing.remove();
				}
				
				// Create the main container
				var container = document.createElement('div');
				container.id = 'viewproof-proof-block';
				
				// Set styles directly
				container.style.position = 'fixed';
				container.style.top = '0';
				container.style.left = '0';
				container.style.width = '100%';
				container.style.backgroundColor = 'blue';
				container.style.color = 'white';
				container.style.padding = '20px';
				container.style.zIndex = '2147483647';
				container.style.fontSize = '24px';
				container.style.display = 'block';
				container.style.visibility = 'visible';
				container.style.opacity = '1';
				
				// Create title
				var title = document.createElement('h2');
				title.innerText = '🤖 VIEWPROOF DATA';
				title.style.fontSize = '30px';
				title.style.margin = '0 0 15px 0';
				title.classList.add('viewproof-title');
				container.appendChild(title);
				
				// Create content
				var pre = document.createElement('pre');
				pre.style.backgroundColor = '#000';
				pre.style.color = '#0f0';
				pre.style.padding = '15px';
				pre.style.margin = '0 auto';
				pre.style.maxWidth = '800px';
				pre.style.border = '3px solid white';
				pre.style.fontSize = '18px';
				pre.style.textAlign = 'left';
				
				// Set content data
				pre.innerText = '';
			`

			// Add each key-value pair
			for key, value := range viewproofData {
				// Escape the values
				escapedKey := escapeJSString(key)
				escapedValue := escapeJSString(value)

				// Append to the script
				script += fmt.Sprintf(`pre.innerText += '%s: %s\\n';`, escapedKey, escapedValue)
			}

			// Close the script
			script += `
				container.appendChild(pre);
				document.body.prepend(container);
				return true;
			})();`

			// Execute the script
			var result bool
			err := chromedp.Evaluate(script, &result).Do(ctx)
			if err != nil {
				log.Printf("ERROR creating ViewProof block: %v", err)
				return err
			}

			log.Printf("Added ViewProof block to proof screenshot")
		}
		return nil
	}))

	// Reduced delay to ensure the block is rendered
	tasks = append(tasks, chromedp.Sleep(1*time.Second)) // Reduced from 2s to 1s

	// Reduced additional delay to ensure all elements are loaded
	tasks = append(tasks, chromedp.Sleep(500*time.Millisecond)) // Reduced from 1s to 500ms

	tasks = append(tasks, chromedp.ActionFunc(func(ctx context.Context) error {
		// Get page metrics
		var metrics map[string]interface{}
		if err := chromedp.Evaluate(`({
			width: Math.max(document.body.scrollWidth, document.documentElement.scrollWidth),
			height: Math.max(document.body.scrollHeight, document.documentElement.scrollHeight),
		})`, &metrics).Do(ctx); err != nil {
			return err
		}

		// Set viewport - keep configured width but use full page height
		// This ensures the page renders at the requested width while capturing the full height
		width := int64(viewport.Width) // Use the configured viewport width

		// Limit height to a maximum value to prevent "Unable to capture screenshot" errors
		// Chrome has issues with extremely tall screenshots
		height := int64(metrics["height"].(float64))
		maxHeight := int64(16384) // Chrome has a limit around 16384 pixels
		if height > maxHeight {
			log.Printf("Warning: Page height (%d) exceeds maximum allowed height (%d). Limiting height.",
				height, maxHeight)
			height = maxHeight
		}

		if err := emulation.SetDeviceMetricsOverride(width, height, 1, false).Do(ctx); err != nil {
			return err
		}

		// Capture full screenshot with error handling
		err := chromedp.CaptureScreenshot(&buf).Do(ctx)
		if err != nil {
			// If we get an error, try with a smaller height
			if height > 8192 {
				log.Printf("Screenshot capture failed, trying with reduced height...")
				if err := emulation.SetDeviceMetricsOverride(width, 8192, 1, false).Do(ctx); err != nil {
					return err
				}
				return chromedp.CaptureScreenshot(&buf).Do(ctx)
			}
			return err
		}

		return nil
	}))

	// Execute tasks
	if err := chromedp.Run(ctx, tasks...); err != nil {
		return err
	}

	// Save screenshot to file
	if err := os.WriteFile(filepath, buf, 0644); err != nil {
		return err
	}

	log.Printf("Captured full-proof screenshot for %s at viewport %dx%d: %s", urlConfig.Name, viewport.Width, viewport.Height, filepath)
	return nil
}

// captureFullPageScreenshot captures a full page screenshot
func (s *Screenshoter) captureFullPageScreenshot(ctx context.Context, urlConfig config.URLConfig, viewport config.Viewport, viewportDir string) error {
	var buf []byte
	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("%s-full-%dx%d.%s", timestamp, viewport.Width, viewport.Height, s.Config.FileFormat)
	filepath := filepath.Join(viewportDir, filename)

	// Create base actions list
	var tasks []chromedp.Action

	// First navigate to the URL
	tasks = append(tasks, chromedp.Navigate(urlConfig.URL))

	// Log cookies before setting our custom ones
	tasks = append(tasks, SaveCookiesToFile(ctx, urlConfig, "before", viewportDir, viewport, "full page"))

	// Extract viewproof data only for saving to companion file, not displaying
	var viewproofData map[string]string
	if len(s.Config.ViewProof) > 0 {
		// Initialize the viewproofData map
		viewproofData = make(map[string]string)

		tasks = append(tasks, chromedp.ActionFunc(func(ctx context.Context) error {
			// Extract cookie values
			cookies, err := storage.GetCookies().Do(ctx)
			if err != nil {
				log.Printf("ERROR: Failed to get cookies for viewproof: %v", err)
				return nil // Non-fatal error
			}

			// Store values of cookies in viewproof list
			for _, cookie := range cookies {
				for _, proofKey := range s.Config.ViewProof {
					if cookie.Name == proofKey {
						viewproofData[fmt.Sprintf("cookie:%s", cookie.Name)] = cookie.Value
					}
				}
			}

			// Extract localStorage values
			for _, proofKey := range s.Config.ViewProof {
				var value string
				err := chromedp.Evaluate(fmt.Sprintf(`localStorage.getItem("%s")`, escapeJSString(proofKey)), &value).Do(ctx)
				if err == nil && value != "" {
					viewproofData[fmt.Sprintf("localStorage:%s", proofKey)] = value
				}
			}

			log.Printf("Extracted %d viewproof values", len(viewproofData))
			return nil
		}))
	}

	// Set cookies and localStorage if specified
	if len(urlConfig.Cookies) > 0 || len(urlConfig.LocalStorage) > 0 {
		tasks = append(tasks, s.setCookiesAndLocalStorage(ctx, urlConfig, viewport, viewportDir, "after", "full page"))
	}

	// Add remaining actions for screenshot - reduce sleep times
	tasks = append(tasks,
		chromedp.Sleep(time.Duration(urlConfig.Delay)*time.Millisecond),

		// Scroll to bottom to load lazy content
		chromedp.Evaluate(`window.scrollTo(0, document.body.scrollHeight)`, nil),
		chromedp.Sleep(500*time.Millisecond), // Reduced from 1s to 500ms

		// Scroll back to top
		chromedp.Evaluate(`window.scrollTo(0, 0)`, nil),
		chromedp.Sleep(500*time.Millisecond), // Reduced from 1s to 500ms
	)

	// Reduced delay to ensure all elements are loaded
	tasks = append(tasks, chromedp.Sleep(1*time.Second)) // Reduced from 2s to 1s

	tasks = append(tasks, chromedp.ActionFunc(func(ctx context.Context) error {
		// Get page metrics
		var metrics map[string]interface{}
		if err := chromedp.Evaluate(`({
			width: Math.max(document.body.scrollWidth, document.documentElement.scrollWidth),
			height: Math.max(document.body.scrollHeight, document.documentElement.scrollHeight),
		})`, &metrics).Do(ctx); err != nil {
			return err
		}

		// Set viewport - keep configured width but use full page height
		// This ensures the page renders at the requested width while capturing the full height
		width := int64(viewport.Width) // Use the configured viewport width

		// Limit height to a maximum value to prevent "Unable to capture screenshot" errors
		// Chrome has issues with extremely tall screenshots
		height := int64(metrics["height"].(float64))
		maxHeight := int64(16384) // Chrome has a limit around 16384 pixels
		if height > maxHeight {
			log.Printf("Warning: Page height (%d) exceeds maximum allowed height (%d). Limiting height.",
				height, maxHeight)
			height = maxHeight
		}

		if err := emulation.SetDeviceMetricsOverride(width, height, 1, false).Do(ctx); err != nil {
			return err
		}

		// Capture full screenshot with error handling
		err := chromedp.CaptureScreenshot(&buf).Do(ctx)
		if err != nil {
			// If we get an error, try with a smaller height
			if height > 8192 {
				log.Printf("Screenshot capture failed, trying with reduced height...")
				if err := emulation.SetDeviceMetricsOverride(width, 8192, 1, false).Do(ctx); err != nil {
					return err
				}
				return chromedp.CaptureScreenshot(&buf).Do(ctx)
			}
			return err
		}

		// If we have ViewProof data, create a companion text file
		if len(s.Config.ViewProof) > 0 && len(viewproofData) > 0 {
			// Create text overlay directly on the image
			overlayText := fmt.Sprintf("VIEWPROOF DATA - %s", timestamp)
			for key, value := range viewproofData {
				overlayText += fmt.Sprintf("\n%s: %s", key, value)
			}

			log.Printf("Adding ViewProof data as direct text overlay on image")
			log.Printf("ViewProof data: %s", overlayText)
		}

		return nil
	}))

	// Execute tasks
	if err := chromedp.Run(ctx, tasks...); err != nil {
		return err
	}

	// Save screenshot to file
	if err := os.WriteFile(filepath, buf, 0644); err != nil {
		return err
	}

	log.Printf("Captured full page screenshot for %s at viewport %dx%d: %s", urlConfig.Name, viewport.Width, viewport.Height, filepath)
	return nil
}

// captureViewportScreenshots captures screenshots divided by viewport
func (s *Screenshoter) captureViewportScreenshots(ctx context.Context, urlConfig config.URLConfig, viewport config.Viewport, viewportDir string, captureViewports bool) error {
	var pageHeight float64
	timestamp := time.Now().Format("20060102-150405")

	// Create base actions list
	var tasks []chromedp.Action

	// First navigate to the URL
	tasks = append(tasks, chromedp.Navigate(urlConfig.URL))

	// Log cookies before setting our custom ones
	tasks = append(tasks, SaveCookiesToFile(ctx, urlConfig, "before-viewport", viewportDir, viewport, "viewport"))

	// Set cookies and localStorage if specified
	if len(urlConfig.Cookies) > 0 || len(urlConfig.LocalStorage) > 0 {
		tasks = append(tasks, s.setCookiesAndLocalStorage(ctx, urlConfig, viewport, viewportDir, "after-viewport", "viewport"))
	}

	// Add remaining actions for screenshot - reduce sleep times
	tasks = append(tasks,
		chromedp.Sleep(time.Duration(urlConfig.Delay)*time.Millisecond),

		// Scroll to bottom to load lazy content
		chromedp.Evaluate(`window.scrollTo(0, document.body.scrollHeight)`, nil),
		chromedp.Sleep(500*time.Millisecond), // Reduced from 1s to 500ms

		// Scroll back to top
		chromedp.Evaluate(`window.scrollTo(0, 0)`, nil),
		chromedp.Sleep(500*time.Millisecond), // Reduced from 1s to 500ms
	)

	tasks = append(tasks, chromedp.Evaluate(`Math.max(document.body.scrollHeight, document.documentElement.scrollHeight)`, &pageHeight))

	// Execute tasks to get page height
	if err := chromedp.Run(ctx, chromedp.Tasks(tasks)); err != nil {
		return err
	}

	// Calculate how many viewport sections we need
	viewportHeight := float64(viewport.Height)

	// For very small viewports, ensure we have a minimum size
	if viewportHeight < 200 {
		log.Printf("Warning: Small viewport height detected (%f). This might cause overlap issues.", viewportHeight)
	}

	// Calculate exact number of full viewports needed
	viewportCount := int(math.Ceil(pageHeight / viewportHeight))

	// Minimum one viewport even for tiny pages
	if viewportCount < 1 {
		viewportCount = 1
	}

	log.Printf("Page height: %f, Viewport height: %f, Will capture %d viewport screenshots",
		pageHeight, viewportHeight, viewportCount)

	// If we have a single viewport or very short page, just take one screenshot
	if pageHeight <= viewportHeight || viewportCount == 1 {
		var buf []byte
		filename := fmt.Sprintf("%s-viewport-%dx%d-1.%s", timestamp, viewport.Width, viewport.Height, s.Config.FileFormat)
		filepath := filepath.Join(viewportDir, filename)

		if err := chromedp.Run(ctx,
			// Scroll to top
			chromedp.Evaluate(`window.scrollTo(0, 0)`, nil),
			chromedp.Sleep(300*time.Millisecond), // Reduced from 500ms to 300ms

			// Set device metrics to capture only viewport
			emulation.SetDeviceMetricsOverride(int64(viewport.Width), int64(viewport.Height), 1, false).
				WithScreenOrientation(&emulation.ScreenOrientation{
					Type:  emulation.OrientationTypePortraitPrimary,
					Angle: 0,
				}),

			// Reduced delay before screenshot to ensure everything is rendered
			chromedp.Sleep(800*time.Millisecond), // Reduced from 1500ms to 800ms

			// Capture screenshot
			chromedp.CaptureScreenshot(&buf),
		); err != nil {
			return err
		}

		// Save screenshot to file
		if err := os.WriteFile(filepath, buf, 0644); err != nil {
			return err
		}

		log.Printf("Captured single viewport screenshot for %s: %s", urlConfig.Name, filepath)
		return nil
	}

	// Use a WaitGroup to capture viewport sections in parallel
	var wg sync.WaitGroup
	// Error channel for parallel processing
	errChan := make(chan error, viewportCount)
	// Semaphore to limit parallel screenshot captures
	vpSem := make(chan struct{}, 4) // Process up to 4 viewport sections in parallel

	// Capture each viewport section with precise positioning
	for i := 0; i < viewportCount; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			// Acquire semaphore
			vpSem <- struct{}{}
			defer func() { <-vpSem }()

			// Calculate exact scroll position for this section
			scrollPos := float64(i) * viewportHeight

			// For the last viewport, ensure we're at the bottom of the page
			if i == viewportCount-1 && scrollPos+viewportHeight > pageHeight {
				scrollPos = pageHeight - viewportHeight
				if scrollPos < 0 {
					scrollPos = 0
				}
			}

			filename := fmt.Sprintf("%s-viewport-%dx%d-%d.%s", timestamp, viewport.Width, viewport.Height, i+1, s.Config.FileFormat)
			filepath := filepath.Join(viewportDir, filename)

			var buf []byte
			// Scroll to position and capture screenshot of only the viewport
			if err := chromedp.Run(ctx,
				// Scroll to position with precise placement
				chromedp.Evaluate(fmt.Sprintf(`window.scrollTo({top: %f, left: 0, behavior: 'instant'})`, scrollPos), nil),
				chromedp.Sleep(300*time.Millisecond), // Reduced from 500ms to 300ms

				// Ensure device metrics are set to capture only viewport
				emulation.SetDeviceMetricsOverride(int64(viewport.Width), int64(viewport.Height), 1, false).
					WithScreenOrientation(&emulation.ScreenOrientation{
						Type:  emulation.OrientationTypePortraitPrimary,
						Angle: 0,
					}),

				// Reduced delay before screenshot to ensure everything is rendered
				chromedp.Sleep(800*time.Millisecond), // Reduced from 1500ms to 800ms

				// Capture only the viewport screenshot
				chromedp.CaptureScreenshot(&buf),
			); err != nil {
				errChan <- err
				return
			}

			// Save screenshot to file
			if err := os.WriteFile(filepath, buf, 0644); err != nil {
				errChan <- err
				return
			}

			log.Printf("Captured viewport screenshot for %s: %s", urlConfig.Name, filepath)
		}(i)
	}

	// Wait for all viewport sections to be captured
	wg.Wait()

	// Check if there were any errors
	select {
	case err := <-errChan:
		return err
	default:
		return nil
	}
}

// extractDomainFromURL extracts a domain name from a URL for cookie setting
func extractDomainFromURL(url string) string {
	// Remove protocol if present
	if strings.HasPrefix(url, "http://") {
		url = url[7:]
	} else if strings.HasPrefix(url, "https://") {
		url = url[8:]
	}

	// Remove www. prefix if present
	if strings.HasPrefix(url, "www.") {
		url = url[4:]
	}

	// Get domain part (stop at first slash)
	if idx := strings.Index(url, "/"); idx > 0 {
		url = url[:idx]
	}

	// Remove port if present
	if idx := strings.Index(url, ":"); idx > 0 {
		url = url[:idx]
	}

	return url
}

// formatViewproofData formats viewproof data for display in the ViewProof block
func formatViewproofData(data map[string]string) string {
	var formattedData strings.Builder
	for key, value := range data {
		formattedData.WriteString(fmt.Sprintf("%s: %s\n", key, value))
	}
	return formattedData.String()
}

// escapeJSString escapes a string for embedding in a JavaScript string
func escapeJSString(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "'", "\\'")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	s = strings.ReplaceAll(s, "\t", "\\t")
	return s
}

// createViewProof creates JavaScript code to inject a ViewProof overlay/block with the provided data
// Parameters:
//   - viewproofData: The data to display in the block
//   - forceful: Whether to use the forceful overlay (true) or standard block (false)
//   - separateCSS: Whether to return CSS separately (for standard block) or include it in the JS (for forceful overlay)
func (s *Screenshoter) createViewProof(viewproofData map[string]string, forceful bool, separateCSS bool) (string, string) {
	// Format the viewproof data for display
	formattedData := formatViewproofData(viewproofData)

	// Element ID based on type
	elementID := "viewproof-block"
	if forceful {
		elementID = "super-viewproof-overlay"
	}

	// Create script based on type
	var script string
	if forceful {
		// Forceful overlay always creates a new element
		script = `
		(function() {
			try {
				// Create element and set properties directly
				var overlay = document.createElement('div');
				overlay.id = '` + elementID + `';
				
				// Apply styles in a cleaner, organized way
				const overlayStyles = {
					// Positioning
					position: 'fixed',
					top: 0,
					left: 0, 
					opacity: 0.9,
					zIndex: '9999',
					// Sizing
					width: '100%',
					boxSizing: 'border-box',
					backgroundColor: 'blue',
					color: 'white',
					padding: '20px',
					border: '1px solid #FFFF00',
					boxShadow: '0 0 50px rgba(0,0,0,0.8)',
					
					// Typography
					fontSize: '30px',
					fontFamily: 'Arial, sans-serif',
					fontWeight: 'bold',
					lineHeight: '1.5',
					textAlign: 'center'
				};
				
				// Apply all styles at once
				Object.assign(overlay.style, overlayStyles);
				
				// Create title with clean styling
				var title = document.createElement('h2');
				title.innerText = '🤖 VIEWPROOF DATA';
				title.classList.add('viewproof-title');
				
				const titleStyles = {
					margin: '0 0 20px 0',
					fontSize: '40px',
					textDecoration: 'underline'
				};
				Object.assign(title.style, titleStyles);
				
				overlay.appendChild(title);
				
				// Create content with clean styling
				var pre = document.createElement('pre');
				
				const preStyles = {
					// Appearance
					backgroundColor: 'black',
					color: '#00FF00',
					border: '5px solid white',
					boxSizing: 'border-box',
					// Spacing
					padding: '15px',
					margin: '10px auto',
					
					// Typography
					fontSize: '20px',
					textAlign: 'left',
					
					// Layout
					maxWidth: '800px',
					overflow: 'visible',
					wordWrap: 'break-word'
				};
				Object.assign(pre.style, preStyles);
				
				pre.innerText = "` + escapeJSString(formattedData) + `";
				overlay.appendChild(pre);
				
				// Add to document
				document.body.prepend(overlay);
				
				console.log('ViewProof overlay created successfully');
				return true;
			} catch(e) {
				console.error('Error creating ViewProof overlay:', e);
				return false;
			}
		})();
		`
	} else {
		// Standard block that checks if one already exists
		script = `
		(function() {
			// Try to find the viewproof block
			let block = document.getElementById('` + elementID + `');
			
			// If block doesn't exist, create it
			if (!block) {
				// Add global style for the block
				let style = document.createElement('style');
				style.textContent = '%s';
				document.head.appendChild(style);
				
				// Create block
				block = document.createElement('div');
				block.id = '` + elementID + `';
				block.innerHTML = '<div class="viewproof-title">🤖 VIEWPROOF DATA</div><pre class="viewproof-content">` + escapeJSString(formattedData) + `</pre>';
				document.body.appendChild(block);
				console.log('ViewProof block created and made visible');
				return true;
			}
			
			// If block exists, ensure it's visible and on top by adding 'viewproof-important' class
			block.className = 'viewproof-important';
			
			// Move to body end to ensure proper stacking
			document.body.appendChild(block);
			
			console.log('ViewProof block visibility enforced');
			return true;
		})();
		`
	}

	// CSS styles for viewproof block
	css := `/* ViewProof Block Styles */
	#` + elementID + ` {
		/* Positioning */
		position: fixed !important;
		top: 0 !important;
		left: 0 !important;
		z-index: 2147483647 !important;
		
		/* Layout */
		width: 100% !important;
		display: block !important;
		visibility: visible !important;
		opacity: 1 !important;
		
		/* Appearance */
		background-color:rgb(0, 55, 255) !important;
		color: white !important;
		padding: 20px !important;
		
		/* Typography */
		font-size: 24px !important;
	}

	
	.viewproof-important {
		position: fixed !important;
		top: 0 !important;
		left: 0 !important;
		width: 100% !important;
		background-color:rgb(255, 0, 251) !important;
		color: white !important;
		padding: 20px !important;
		font-size: 24px !important;
		z-index: 2147483647 !important;
		display: block !important;
		visibility: visible !important;
		opacity: 1 !important;
	}
	
	#` + elementID + ` pre {
		/* Appearance */
		background-color: black !important;
		color: #00FF00 !important;
		border: 5px solid white !important;
		
		/* Layout */
		max-width: 800px !important;
		overflow: visible !important;
		word-wrap: break-word !important;
		
		/* Spacing */
		padding: 15px !important;
		margin: 10px auto !important;
		
		/* Typography */
		font-size: 20px !important;
		text-align: left !important;
	}
	
	.viewproof-title {
		font-size: 22px !important;
		font-weight: bold !important;
		margin-bottom: 10px !important;
		text-align: center !important;
		width: 100% !important;
		display: block !important;
	}
	
	.viewproof-content {
		text-align: left !important;
		background: black !important;
		padding: 10px !important;
		margin: 0 !important;
	}`

	// If not separating CSS, script is returned as is
	if !separateCSS {
		return script, ""
	}

	// Otherwise return both script and CSS separately
	return script, css
}

// CaptureURLs captures screenshots for all URLs in configuration
func (s *Screenshoter) CaptureURLs(ctx context.Context) error {
	// Create semaphore to limit concurrency
	sem := make(chan struct{}, s.Config.Concurrency)
	errChan := make(chan error, len(s.Config.URLs))
	doneChan := make(chan struct{}, len(s.Config.URLs))

	// Process each URL
	for _, urlConfig := range s.Config.URLs {
		urlConfig := urlConfig // Create local copy for goroutine

		// Acquire semaphore
		sem <- struct{}{}

		// Start goroutine to process URL
		go func() {
			defer func() {
				// Release semaphore when done
				<-sem
				doneChan <- struct{}{}
			}()

			// Capture URL
			if err := s.CaptureURL(ctx, urlConfig); err != nil {
				errChan <- fmt.Errorf("error capturing URL %s: %w", urlConfig.Name, err)
			}
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < len(s.Config.URLs); i++ {
		<-doneChan
	}

	// Check if there were any errors
	select {
	case err := <-errChan:
		return err
	default:
		return nil
	}
}
