# Sitesmith

A free, simple website builder. It runs as one program on your own computer and publishes plain static files that you can host anywhere, including on free hosts.

**Status:** an MVP to find out what works and what needs more thought.

## Run it

```bash
go run .
```

Then open the editor at http://127.0.0.1:4321 (the address is also printed when it starts).

### Several sites

Click the site name in the top bar to open **Your sites**. From there you can switch to another site, start a **New site** (which runs the setup wizard), or remove one. One site is open at a time, and switching saves your changes first.

Everything lives in `./site-data/`:

| Path | What |
|---|---|
| `sites/<id>/site.db` | All of a site's content (SQLite). Back up a site's folder and you've backed up that site. |
| `sites/<id>/media/` | Its uploaded images, resized |
| `sites/<id>/published/` | Its last published build: plain HTML/CSS/images |
| `trash/` | Removed sites. Move a folder back into `sites/` to restore it. |
| `state.json` | Which site was open last |

A data folder from before multi-site support (with `site.db` at the top level) is moved into `sites/` automatically the first time you start Sitesmith.

Flags: `-data <folder>`, `-addr 127.0.0.1:4321`, `-open` (also open the editor in your browser).

### Building a release

```bash
go run ./tools/build
```

This writes `dist/sitesmith-<os>-<arch>`: one file with no other runtime dependencies. The admin UI, the theme and the self-hosting web servers (below) are all embedded in it. Add `-goos linux -goarch arm64` (or similar) to build for another platform.

A plain `go build` or `go run .` also works, but leaves out the web servers, so the export dialog says they aren't included. Run the build script once and `go run .` picks them up as well.

### Hosting it yourself

In the Publish dialog, tick **Include a web server** before **Download as ZIP**, and pick the computer it will run on. The download then contains:

```
site/                 the website
website-server(.exe)  a small program that hosts it
README.txt            how to start it, written for that platform
```

Running `website-server` serves the `site` folder next to it on port 8080, or the next free port. It prints the address on this computer and on the local network. Flags: `-port` (or `$PORT`), `-local`, `-dir`, `-open`. It serves clean URLs, the 404 page and sensible cache headers, and refuses folder listings, hidden files and anything outside `site`.

The server (`cmd/website-server`) uses only Go's standard library. `tools/build` compiles it for Windows, Mac (Apple silicon and Intel), Linux and Raspberry Pi. It compresses each copy into `internal/serverbin/bin/`, which git ignores, so no binaries live in the repo.

Run the tests with `go test ./...`.

## How it works

```
Editor (Go server + SQLite + admin UI)  ──Publish──►  published/  (static files, no server code)
```

- **The editor app** (`internal/server`) has a JSON API, the admin UI (`web/admin`, plain JS modules, no build step) and a live preview at `/preview/…`. It only accepts connections from this computer.
- **Publishing** (`internal/build`) renders every published page to `path/index.html` with **relative links**. That way the folder works on any host, including sub-paths like GitHub Pages project sites.
- **The preview and the build share one renderer** (`internal/render`). They differ only in how URLs are written.

### Design decisions worth knowing

- **The page tree is the menu.** Top-level pages are menu items, and their children become dropdowns. Order in the sidebar is the menu order. "Show in menu" hides a page from the menu without unpublishing it.
- **A draft hides its whole branch.** Subpages of a draft page aren't published either.
- **Internal links are stored as `page:<id>`**, not as URLs, so renaming or moving a page never breaks a link. Publishing warns if a link points to a draft or deleted page.
- **Redirects only for URLs the public actually saw.** Each publish records the live paths. If a page's URL later changes, the next publish writes a redirect from the old URL. Renaming a page five times before publishing creates no junk redirects.
- **Sections, not pixels.** Users pick *what* (Hero, Columns, Gallery…). The theme decides *how it looks*. The block schema in `internal/blocks` is the single source of truth: the UI builds its forms from it, the API validates and sanitises against it, and the renderer resolves values with it. Adding a section type means one Go struct plus one template.
- **The theme is design tokens.** Five font styles, three corner styles, two spacing options, light/dark, and any colour. Link colours are automatically darkened or lightened until they're readable, so every combination works.
- **Fine-tuning per section, in steps.** Each section has a collapsed **Style** drawer: space above/below (None to XL), height (hero and call to action: Short to Full screen), background (Plain, Soft, Brand, Contrast) and content width (Narrow to Full). Every control starts at *Auto*, which keeps the theme's look, and only the choices you change are stored. Backgrounds swap the theme's colour variables inside the section, so text, cards, links and buttons adapt without extra rules.
- **Page titles are optional.** Pages show their name as a heading unless they start with a Hero (whose headline takes its place) or have **Show title** switched off. A switched-off title stays in the page as a hidden `<h1>`, so search engines and screen readers still get it.
- **Starter content is coaching.** The onboarding wizard creates real pages whose placeholder text explains what to write.
- **Image processing on upload:** photos are rotated upright (EXIF orientation), resized to 480/960/1600/2400 widths, re-encoded (which strips GPS metadata), and served with `srcset`.
- **Autosave, but nothing counts until you publish.** Edits save about 0.6s after you stop typing, so nothing is ever lost. The live site only changes when you click Publish. The editor marks each page with unpublished changes, and the Publish button shows how many there are.
- **Versions are whole-site snapshots** of pages, menu, design and settings, so they can undo anything. One is taken automatically before a change whenever the last one is over 5 minutes old, which means any moment of editing is at most 5 minutes from a version. Versions are also taken on every publish, before every restore, and whenever you click "Save version". Automatic ones are thinned over time: all from the last day are kept, then one per day for a month, then one per week. Saved and published versions are kept forever.
- **Going back never loses work by surprise.** "Discard unpublished changes", "Undo changes to this page" and "Go back to this" check whether what you have now is already in a version. If it is, you get a simple confirm. If not, Sitesmith asks: *Save and go back*, *Go back without saving*, or *Cancel*. The Versions list marks where you are ("You're here"), whether that's one of the versions or unsaved work.
- **Deleting an image only hides it**, so going back to an older version brings it back. Its files stay on disk.

## What's in the MVP

- Onboarding wizard with 6 starter sites (business, portfolio, restaurant, organisation, personal, blank)
- Page tree with drag-and-drop nesting and reordering, drafts, menu visibility, home page selection, duplicate, delete
- 12 section types: hero, heading, text, image, columns, button, call to action, gallery, quote, video (YouTube/Vimeo, privacy-enhanced), subpage list, divider
- Live preview that updates in place, with desktop and phone views. Clicking a section in the preview selects it in the editor, and clicking a link in the preview opens that page.
- Design panel: logo, colour, fonts, corners, spacing, light/dark
- Media library with upload, drag-and-drop, and a warning before deleting an image that's in use
- Publish: build report with warnings, "View published site", "Download as ZIP", "Open folder", free hosting tips
- SEO basics: per-page description, Open Graph tags, canonical URLs, sitemap.xml and robots.txt (when a website address is set), 404 page, breadcrumbs

## Open questions: things that need more thinking

**Publishing is the weakest step for non-technical users.** "Here's a folder, upload it somewhere" is still a hurdle. Options:
1. One-click deploy integrations (Netlify / Cloudflare Pages / GitHub Pages APIs). This needs OAuth or tokens, and those are external accounts.
2. Self-hosting. *Done in a basic form:* a download can include `website-server` (see "Hosting it yourself"). It speaks plain HTTP only, so the next step is built-in HTTPS (automatic Let's Encrypt certificates when given a domain name). Someone still pays for the machine it runs on.
3. A community-run free host. This is the closest to "free for everyone", but it's an ongoing cost and an abuse-handling problem.

**Local app vs. hosted editor.** The editor currently runs on one computer, which is simple, private and free. But you can't edit from your phone, and two people can't work on the same site. A hosted multi-user mode needs auth, roles, and conflict handling.

**Contact forms.** These are the most-requested dynamic feature, and a static site can't process forms on its own. Options: a `mailto:` button (works today via links), a third-party form service, or a small form endpoint in a self-hosted Sitesmith.

**Blog / news posts.** The data model has room for a "collection" (dated pages with a listing and RSS), but the UX needs design: where posts live in the tree, and how listings paginate.

**Navigation edge cases:**
- The menu shows two levels. Deeper pages are reachable via breadcrumbs and subpage lists, but not from the menu. Is that the right limit?
- The home page's subpages live under `/home/…`, and `/home/` redirects to `/`.
- Desktop dropdowns open on hover/focus. On tablets with wide screens, tapping a parent item navigates instead of opening its dropdown.
- Big menus (10+ top-level pages) wrap onto a second row.

**Editor UX:**
- Editing happens in forms beside the preview, not by typing directly on the page. Direct on-page editing would feel more like "what you see is what you get", but it's much more complex.
- There's no step-by-step undo (Ctrl+Z across sections). Versions and the undo after deleting a section cover the big mistakes.
- Deleted images are never removed from disk. A "clean up unused files" action should delete files that no version uses any more.
- There are no branches (parallel lines of work you can switch between). "You're here" covers knowing where you are; branching is worth it only if people need to keep two alternatives going at once.
- There's no experiment mode yet: you can't try something in a separate copy and then keep it or throw it away. Versions plus Discard cover most of this, so it's worth adding only if it turns out to be missed.
- The editor UI is desktop-only. It hides the preview below 1100px.

**Theme extensibility.** There's one built-in theme. Should more themes be token presets (which keeps everything compatible), or full template sets (more variety, but harder to keep every section type looking right)?

**Images:**
- No WebP/AVIF output yet. Go's standard library can't encode them without cgo or a WASM encoder.
- Animated GIFs are kept as-is and not resized.
- No focal point or crop control for hero and column images.

**Builds** re-render everything. That's fine for hundreds of pages, but incremental builds would matter for big sites.

**Accessibility review.** The basics are there: semantic HTML, a skip link, alt text fields, keyboard-reachable controls, contrast-checked link colours. The admin UI has had no screen-reader testing.

## Project layout

```
main.go                 starts the editor
cmd/website-server      the small server that ships inside self-hosting downloads
tools/build             release build: compiles the servers, then Sitesmith
internal/store          SQLite: site settings, page tree, versions, media, redirects
internal/blocks         section schema, validation, sanitising
internal/render         theme templates + CSS tokens; preview and static rendering
internal/build          static build, redirects, sitemap, ZIP export
internal/media          image processing (orientation, resizing, metadata stripping)
internal/starter        onboarding starter sites
internal/server         HTTP API, preview, local-only guard, multiple sites
internal/serverbin      the compiled website servers, embedded for export
web/admin               the editor UI (vanilla JS modules, embedded in the binary)
```

## License

MIT — see [LICENSE](LICENSE). Free to use, change and share, including commercially.
