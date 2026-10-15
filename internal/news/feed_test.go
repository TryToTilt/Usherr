package news

import "testing"

func TestParseRSS(t *testing.T) {
	doc := `<?xml version="1.0"?><rss version="2.0" xmlns:dc="http://purl.org/dc/elements/1.1/"><channel><title>X</title>
<item><title>Dune &amp; more</title><link>https://ex.com/a</link><guid>g1</guid><pubDate>Mon, 12 Oct 2026 07:43:16 +0000</pubDate>
<description><![CDATA[<p>The <b>sequel</b> is coming.</p><p>The post Dune appeared first on Ex.</p>]]></description></item>
</channel></rss>`
	items, err := Parse([]byte(doc))
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%v err=%v", items, err)
	}
	it := items[0]
	if it.Title != "Dune & more" || it.Link != "https://ex.com/a" || it.GUID != "g1" || it.Published.IsZero() {
		t.Fatalf("got %+v", it)
	}
	if it.Teaser != "The sequel is coming." {
		t.Fatalf("teaser %q", it.Teaser)
	}
}

func TestParseAtom(t *testing.T) {
	doc := `<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom"><title>V</title>
<entry><title type="html">A &lt;i&gt;new&lt;/i&gt; TV</title><id>tag:1</id><link rel="alternate" type="text/html" href="https://v.com/1"/>
<published>2026-10-11T22:31:45+00:00</published><summary type="html">&lt;p&gt;OLED, brighter.&lt;/p&gt;</summary></entry></feed>`
	items, err := Parse([]byte(doc))
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%v err=%v", items, err)
	}
	it := items[0]
	if it.Title != "A new TV" || it.Link != "https://v.com/1" || it.Teaser != "OLED, brighter." || it.Published.IsZero() {
		t.Fatalf("got %+v", it)
	}
}

func TestParseRejectsHTML(t *testing.T) {
	if _, err := Parse([]byte(`<html><body>hi</body></html>`)); err == nil {
		t.Fatal("an HTML page should not parse as a feed")
	}
}

func TestParseLatin1(t *testing.T) {
	doc := append([]byte(`<?xml version="1.0" encoding="ISO-8859-1"?><rss version="2.0"><channel><item><title>Caf`), 0xE9)
	doc = append(doc, []byte(` TV</title><link>https://f.com/1</link><description>ok</description></item></channel></rss>`)...)
	items, err := Parse(doc)
	if err != nil || len(items) != 1 || items[0].Title != "Café TV" {
		t.Fatalf("items=%+v err=%v", items, err)
	}
}
