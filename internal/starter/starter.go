// Package starter creates a ready-made site for the onboarding wizard, so
// nobody has to start from a blank page. Placeholder text doubles as advice on
// what to write.
package starter

import (
	"encoding/json"
	"fmt"
	"regexp"

	"sitesmith/internal/blocks"
	"sitesmith/internal/store"
)

type Kind struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	Primary     string `json:"primary"`
	Fonts       string `json:"fonts"`
	pages       func(name string) []page
}

type page struct {
	title    string
	desc     string
	hidden   bool
	blocks   []map[string]any
	children []page
}

func b(t string, data map[string]any) map[string]any {
	return map[string]any{"type": t, "data": data}
}

func hero(heading, text, label, link string) map[string]any {
	return b("hero", map[string]any{"heading": heading, "text": text, "buttonLabel": label, "buttonLink": link})
}

func text(html string) map[string]any { return b("text", map[string]any{"html": html}) }

func heading(t string) map[string]any { return b("heading", map[string]any{"text": t}) }

func cta(h, t, label, link string) map[string]any {
	return b("cta", map[string]any{"heading": h, "text": t, "buttonLabel": label, "buttonLink": link})
}

func quote(t, who string) map[string]any { return b("quote", map[string]any{"text": t, "cite": who}) }

func cols(items ...[3]string) map[string]any {
	list := []any{}
	for _, it := range items {
		list = append(list, map[string]any{"heading": it[0], "html": "<p>" + it[1] + "</p>", "link": it[2], "linkLabel": ifLink(it[2])})
	}
	return b("columns", map[string]any{"items": list})
}

func ifLink(l string) string {
	if l == "" {
		return ""
	}
	return "Learn more"
}

func subpages() map[string]any { return b("subpages", map[string]any{"style": "cards"}) }

func contactPage(intro string) page {
	return page{title: "Contact", desc: "How to get in touch.", blocks: []map[string]any{
		text("<p>" + intro + "</p>"),
		cols(
			[3]string{"Email", "hello@example.com", ""},
			[3]string{"Phone", "+1 555 0100", ""},
			[3]string{"Address", "123 Example Street, Your Town", ""},
		),
	}}
}

var Kinds = []Kind{
	{
		ID: "business", Label: "Business", Icon: "briefcase", Primary: "#3b5bdb", Fonts: "modern",
		Description: "Services, about, and contact pages for a small business or freelancer.",
		pages: func(name string) []page {
			return []page{
				{title: "Home", blocks: []map[string]any{
					hero("Say what you do, in one sentence", "Then add a line about who it's for and why they should pick you. Keep it short — visitors decide in seconds.", "Get in touch", "link:contact"),
					heading("What we offer"),
					cols(
						[3]string{"Your first service", "Describe it in a sentence or two. Focus on what the customer gets.", "link:services"},
						[3]string{"Your second service", "People skim, so lead with the benefit.", "link:services"},
						[3]string{"Your third service", "Not sure what to write? Imagine explaining it to a friend.", "link:services"},
					),
					quote("A short quote from a happy customer builds more trust than anything you can say about yourself.", "Customer name, Company"),
					cta("Ready to talk?", "Tell visitors exactly what to do next.", "Contact us", "link:contact"),
				}},
				{title: "About", desc: "Who we are and how we work.", blocks: []map[string]any{
					text("<p>Tell your story: how you started, what you care about, and what makes you different. People buy from people, so it's fine to be personal.</p><p>A photo of you or your team works wonders here — add an <strong>Image</strong> section below.</p>"),
				}},
				{title: "Services", desc: "Everything we can help you with.", blocks: []map[string]any{
					text("<p>A short introduction to what you offer. Each service has its own page, listed below automatically.</p>"),
					subpages(),
				}, children: []page{
					{title: "First service", desc: "A one-line summary that appears on the Services page.", blocks: []map[string]any{
						text("<p>Explain what this service includes, who it's for, and what it costs (or how to get a quote).</p>"),
						b("button", map[string]any{"label": "Ask about this", "link": "link:contact"}),
					}},
					{title: "Second service", desc: "A one-line summary that appears on the Services page.", blocks: []map[string]any{
						text("<p>Explain what this service includes, who it's for, and what it costs (or how to get a quote).</p>"),
						b("button", map[string]any{"label": "Ask about this", "link": "link:contact"}),
					}},
				}},
				contactPage("We'd love to hear from you. Replace these details with your own."),
			}
		},
	},
	{
		ID: "portfolio", Label: "Portfolio", Icon: "camera", Primary: "#212529", Fonts: "editorial",
		Description: "Show off your work: photography, design, art, writing.",
		pages: func(name string) []page {
			return []page{
				{title: "Home", blocks: []map[string]any{
					hero("Hi, I'm "+name+".", "One line about the work you do and the kind of projects you love.", "See my work", "link:work"),
					b("gallery", map[string]any{"columns": "3"}),
					cta("Have a project in mind?", "", "Get in touch", "link:contact"),
				}},
				{title: "Work", desc: "Selected projects.", blocks: []map[string]any{
					subpages(),
				}, children: []page{
					{title: "Project one", desc: "A one-line summary of the project.", blocks: []map[string]any{
						text("<p>What was the brief? What did you do? What came of it? A few short paragraphs and lots of pictures work best.</p>"),
						b("gallery", map[string]any{"columns": "2"}),
					}},
					{title: "Project two", desc: "A one-line summary of the project.", blocks: []map[string]any{
						text("<p>What was the brief? What did you do? What came of it?</p>"),
						b("gallery", map[string]any{"columns": "2"}),
					}},
				}},
				{title: "About", desc: "A little about me.", blocks: []map[string]any{
					text("<p>Write about yourself in a friendly, relaxed way: your background, what inspires you, and who you've worked with.</p>"),
				}},
				contactPage("Available for commissions and collaborations."),
			}
		},
	},
	{
		ID: "restaurant", Label: "Restaurant or café", Icon: "utensils", Primary: "#c2410c", Fonts: "classic",
		Description: "Menu, opening hours and directions — what hungry visitors look for.",
		pages: func(name string) []page {
			return []page{
				{title: "Home", blocks: []map[string]any{
					hero("Welcome to "+name, "A sentence about your food and atmosphere. Add a mouth-watering photo as the background.", "See the menu", "link:menu"),
					cols(
						[3]string{"Opening hours", "Mon–Fri 8:00–22:00<br>Sat–Sun 9:00–23:00", ""},
						[3]string{"Find us", "123 Example Street, Your Town", "link:visit"},
						[3]string{"Book a table", "Call +1 555 0100 or pop in.", "link:visit"},
					),
					quote("The best brunch in town. We come back every weekend.", "A regular"),
				}},
				{title: "Menu", desc: "What we serve.", blocks: []map[string]any{
					heading("Starters"),
					text("<p><strong>Dish name</strong> — short description · 8.50</p><p><strong>Dish name</strong> — short description · 7.00</p>"),
					heading("Mains"),
					text("<p><strong>Dish name</strong> — short description · 16.00</p><p><strong>Dish name</strong> — short description · 14.50</p>"),
					heading("Desserts"),
					text("<p><strong>Dish name</strong> — short description · 6.50</p>"),
				}},
				{title: "About us", desc: "Our story.", blocks: []map[string]any{
					text("<p>How did it all start? Who's in the kitchen? Where do your ingredients come from? Guests love a good story.</p>"),
				}},
				{title: "Visit", desc: "Opening hours and how to find us.", blocks: []map[string]any{
					cols(
						[3]string{"Address", "123 Example Street<br>Your Town", ""},
						[3]string{"Opening hours", "Mon–Fri 8:00–22:00<br>Sat–Sun 9:00–23:00", ""},
						[3]string{"Reservations", "+1 555 0100<br>hello@example.com", ""},
					),
				}},
			}
		},
	},
	{
		ID: "organization", Label: "Club or organisation", Icon: "users", Primary: "#0f766e", Fonts: "friendly",
		Description: "For clubs, charities, schools, churches and community groups.",
		pages: func(name string) []page {
			return []page{
				{title: "Home", blocks: []map[string]any{
					hero(name, "A sentence about what your group does and who can join.", "Get involved", "link:get-involved"),
					cols(
						[3]string{"Who we are", "A quick introduction.", "link:about"},
						[3]string{"What's on", "Upcoming events and regular meetings.", "link:events"},
						[3]string{"Join us", "How to become a member or volunteer.", "link:get-involved"},
					),
				}},
				{title: "About", desc: "Who we are.", blocks: []map[string]any{
					text("<p>An overview of your organisation. More detail lives on the pages below.</p>"),
					subpages(),
				}, children: []page{
					{title: "Our story", desc: "How we started and what we stand for.", blocks: []map[string]any{
						text("<p>When were you founded, and why? What do you hope to achieve?</p>"),
					}},
					{title: "Our team", desc: "The people who make it happen.", blocks: []map[string]any{
						cols(
							[3]string{"Name", "Role — a sentence about them.", ""},
							[3]string{"Name", "Role — a sentence about them.", ""},
							[3]string{"Name", "Role — a sentence about them.", ""},
						),
					}},
				}},
				{title: "Events", desc: "What's coming up.", blocks: []map[string]any{
					heading("Regular meetings"),
					text("<p>Every Tuesday, 19:00 at the community hall.</p>"),
					heading("Coming up"),
					text("<ul><li><strong>12 March</strong> — Spring meetup</li><li><strong>4 May</strong> — Open day</li></ul>"),
				}},
				{title: "Get involved", desc: "Join, volunteer or donate.", blocks: []map[string]any{
					text("<p>Explain how people can join, volunteer or support you, and what to expect.</p>"),
					b("button", map[string]any{"label": "Contact us", "link": "link:contact"}),
				}},
				contactPage("Questions? Get in touch."),
			}
		},
	},
	{
		ID: "personal", Label: "Personal", Icon: "user", Primary: "#7048e8", Fonts: "modern",
		Description: "A simple home on the web for you, your CV or your hobby.",
		pages: func(name string) []page {
			return []page{
				{title: "Home", blocks: []map[string]any{
					hero("Hello, I'm "+name+".", "A sentence or two about who you are and what you're up to.", "About me", "link:about"),
				}},
				{title: "About", desc: "About me.", blocks: []map[string]any{
					text("<p>Write a little about yourself: what you do, what you enjoy, and what you're working on right now.</p>"),
				}},
				contactPage("The best ways to reach me."),
			}
		},
	},
	{
		ID: "blank", Label: "Start from scratch", Icon: "file", Primary: "#3b5bdb", Fonts: "modern",
		Description: "Just a home page. Build everything yourself.",
		pages: func(name string) []page {
			return []page{{title: "Home", blocks: []map[string]any{hero("Welcome to "+name, "", "", "")}}}
		},
	},
}

func Find(id string) (Kind, bool) {
	for _, k := range Kinds {
		if k.ID == id {
			return k, true
		}
	}
	return Kind{}, false
}

var linkRef = regexp.MustCompile(`link:([a-z0-9-]+)`)

// Apply creates the starter pages and marks setup as done.
func Apply(st *store.Store, k Kind, site store.Site) error {
	pages := k.pages(site.Name)
	ids := map[string]int64{}
	created := map[int64]page{}
	var create func(parent int64, list []page) error
	create = func(parent int64, list []page) error {
		for _, p := range list {
			pg, err := st.CreatePage(parent, p.title, nil)
			if err != nil {
				return err
			}
			ids[store.Slugify(p.title)] = pg.ID
			created[pg.ID] = p
			if err := create(pg.ID, p.children); err != nil {
				return err
			}
		}
		return nil
	}
	if err := create(0, pages); err != nil {
		return err
	}

	// Second pass: now every page has an id, point "link:slug" at it.
	for id, p := range created {
		raw, err := json.Marshal(p.blocks)
		if err != nil {
			return err
		}
		raw = linkRef.ReplaceAllFunc(raw, func(m []byte) []byte {
			if target, ok := ids[string(m[5:])]; ok {
				return []byte(fmt.Sprintf("page:%d", target))
			}
			return nil
		})
		clean, err := blocks.Normalize(raw)
		if err != nil {
			return err
		}
		desc := p.desc
		show := !p.hidden
		if _, err := st.UpdatePage(id, store.PageUpdate{Blocks: &clean, Description: &desc, ShowInMenu: &show}); err != nil {
			return err
		}
	}

	cur, err := st.Site()
	if err != nil {
		return err
	}
	site.HomePageID = cur.HomePageID
	site.SetupDone = true
	return st.SaveSite(site)
}
