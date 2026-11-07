package ebookconv

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

type entry struct{ name, body string }

func zipOf(t *testing.T, entries []entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for i, e := range entries {
		method := zip.Deflate
		if i == 0 {
			method = zip.Store
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: e.name, Method: method})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, e.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func unzip(t *testing.T, data []byte) ([]string, map[string]string, zip.FileHeader) {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	bodies := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(rc)
		rc.Close()
		names = append(names, f.Name)
		bodies[f.Name] = string(b)
	}
	return names, bodies, zr.File[0].FileHeader
}

func TestFixForKindleMendsACopy(t *testing.T) {
	book := []entry{
		{"mimetype", "application/epub+zip"},
		{"OEBPS/content.opf", `<package><metadata xmlns:dc="x"><dc:title>T</dc:title></metadata></package>`},
		{"OEBPS/toc.ncx", `<ncx><navPoint><content src="ch1.xhtml#start"/></navPoint><navPoint><content src="ch2.xhtml#sec"/></navPoint></ncx>`},
		{"OEBPS/ch1.xhtml", `<html><body id="start"><img alt="gone"/><img src="a.png"/><p>Hi</p></body></html>`},
		{"OEBPS/ch2.xhtml", `<?xml version="1.0"?><html><body><p id="sec">There</p></body></html>`},
		{"OEBPS/a.png", "not really"},
	}
	data := zipOf(t, book)
	out, fixed, err := FixForKindle(data)
	if err != nil {
		t.Fatal(err)
	}
	names, bodies, first := unzip(t, out)
	if strings.Join(names, ",") != "mimetype,OEBPS/content.opf,OEBPS/toc.ncx,OEBPS/ch1.xhtml,OEBPS/ch2.xhtml,OEBPS/a.png" {
		t.Errorf("order changed: %v", names)
	}
	if first.Method != zip.Store {
		t.Error("mimetype must stay uncompressed")
	}
	if !strings.Contains(bodies["OEBPS/content.opf"], "<dc:language>en</dc:language></metadata>") {
		t.Errorf("no language: %s", bodies["OEBPS/content.opf"])
	}
	if !strings.HasPrefix(bodies["OEBPS/ch1.xhtml"], "<?xml") {
		t.Error("no declaration on the first page")
	}
	if strings.Contains(bodies["OEBPS/ch1.xhtml"], "gone") || !strings.Contains(bodies["OEBPS/ch1.xhtml"], `src="a.png"`) {
		t.Errorf("images: %s", bodies["OEBPS/ch1.xhtml"])
	}
	if bodies["OEBPS/ch2.xhtml"] != book[4].body {
		t.Error("a good page must stay as it is")
	}
	if !strings.Contains(bodies["OEBPS/toc.ncx"], `src="ch1.xhtml"`) || !strings.Contains(bodies["OEBPS/toc.ncx"], `src="ch2.xhtml#sec"`) {
		t.Errorf("contents: %s", bodies["OEBPS/toc.ncx"])
	}
	if len(fixed) != 4 {
		t.Errorf("fixed: %v", fixed)
	}
}

func TestFixForKindleLeavesAGoodBookAlone(t *testing.T) {
	data := zipOf(t, []entry{
		{"mimetype", "application/epub+zip"},
		{"content.opf", `<package><metadata><dc:language>nl</dc:language></metadata></package>`},
		{"a.xhtml", `<?xml version="1.0"?><html><body><img src="x.png"/></body></html>`},
		{"META-INF/encryption.xml", `<encryption><EncryptedData><EncryptionMethod Algorithm="http://www.idpf.org/2008/embedding"/></EncryptedData></encryption>`},
	})
	out, fixed, err := FixForKindle(data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, data) || len(fixed) != 0 {
		t.Errorf("changed: %v", fixed)
	}
}

func TestFixForKindleSetsAnUnknownLanguage(t *testing.T) {
	data := zipOf(t, []entry{{"mimetype", "x"}, {"c.opf", `<metadata><dc:language>und</dc:language></metadata>`}})
	out, _, err := FixForKindle(data)
	if err != nil {
		t.Fatal(err)
	}
	if _, bodies, _ := unzip(t, out); bodies["c.opf"] != `<metadata><dc:language>en</dc:language></metadata>` {
		t.Errorf("got %s", bodies["c.opf"])
	}
}

func TestFixForKindleRefusesDRM(t *testing.T) {
	data := zipOf(t, []entry{
		{"mimetype", "x"},
		{"META-INF/encryption.xml", `<encryption><EncryptedData><EncryptionMethod Algorithm="http://www.w3.org/2001/04/xmlenc#aes128-cbc"/></EncryptedData></encryption>`},
	})
	if _, _, err := FixForKindle(data); !errors.Is(err, ErrDRM) {
		t.Errorf("got %v", err)
	}
}
