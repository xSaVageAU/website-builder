package build

import (
	"strings"
)

// SelfHostReadme is the "how to start it" note that ships next to the
// website server in an export. goos picks the instructions that apply.
func SelfHostReadme(siteName, goos, server string) []byte {
	var b strings.Builder
	line := func(s string) { b.WriteString(s + "\n") }

	line(siteName)
	line(strings.Repeat("=", len(siteName)))
	line("")
	line("This folder is your website.")
	line("")
	line("  site/                The website itself: plain HTML, CSS and images.")
	line("  " + server + strings.Repeat(" ", max(2, 21-len(server))) + "A small program that hosts it.")
	line("")
	line("")
	line("START THE WEBSITE")
	line("")
	switch goos {
	case "windows":
		line("Double-click " + server + ".")
		line("")
		line("Windows may warn that it's from an unknown publisher. Click \"More info\",")
		line("then \"Run anyway\". If Windows asks about network access, allow it so")
		line("other devices can visit the site.")
	case "darwin":
		line("Double-click " + server + ".")
		line("")
		line("The first time, macOS may say it can't check the program for malware,")
		line("because it doesn't come from the App Store. To allow it, open System")
		line("Settings > Privacy & Security, scroll down and click \"Open Anyway\".")
		line("Or, in Terminal, in this folder:")
		line("")
		line("    xattr -d com.apple.quarantine " + server)
		line("    ./" + server)
	default:
		line("In a terminal, in this folder:")
		line("")
		line("    ./" + server)
		line("")
		line("If it says \"permission denied\", run: chmod +x " + server)
	}
	line("")
	line("It shows the address to visit, usually http://localhost:8080/")
	line("Phones and other computers on the same network can use the")
	line("\"On your network\" address it shows.")
	line("")
	line("Leave it running while the site should be online. Close the window")
	line("(or press Ctrl+C) to stop it.")
	line("")
	line("")
	line("UPDATING THE SITE")
	line("")
	line("Publish again in Sitesmith, download a new copy, and replace the site")
	line("folder. The server picks up the new files straight away.")
	line("")
	line("")
	line("OPTIONS")
	line("")
	line("    -port 80        Use another port (or set the PORT environment variable).")
	line("    -local          Only allow visitors from this computer.")
	line("    -no-browser     Don't open a browser when starting.")
	line("    -dir <folder>   Serve a different folder.")
	line("")
	line("")
	line("PUTTING IT ON THE INTERNET")
	line("")
	line("For visitors from anywhere, the computer running the server needs to be")
	line("reachable from the internet: usually a rented server (VPS), or a home")
	line("computer with port forwarding set up on the router. The server speaks")
	line("plain HTTP. For HTTPS (the padlock), put it behind a service such as")
	line("Cloudflare Tunnel or a web server like Caddy.")
	line("")
	line("You don't need this program to publish the site elsewhere: the site")
	line("folder works as-is on any web host.")

	out := b.String()
	if goos == "windows" {
		out = strings.ReplaceAll(out, "\n", "\r\n")
	}
	return []byte(out)
}
