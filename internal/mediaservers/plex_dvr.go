package mediaservers

// Usherr: Live TV in Plex. Plex's own Live TV & DVR (Plex Pass) takes a
// network tuner (an HDHomeRun, or something answering as one) and a guide.
// These are the calls Plex's own setup makes: add the tuner by its address,
// make a DVR of it with an XMLTV guide, and load the guide. Plex answers
// these in JSON or XML (whatever it was asked), sometimes with nothing at
// all, so the answers are read either way and an empty one is no error.

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// PlexDVR is one of Plex's DVRs: its tuners and its guide.
type PlexDVR struct {
	Key     string
	Lineup  string
	Devices []PlexTuner
}

// PlexTuner is a tuner Plex knows.
type PlexTuner struct {
	UUID string
	URI  string
}

// plexLiveDoc reads Plex's Live TV answers, JSON or XML.
type plexLiveDoc struct {
	XMLName xml.Name `xml:"MediaContainer" json:"-"`
	Dvr     []struct {
		Key    string `xml:"key,attr" json:"key"`
		Lineup string `xml:"lineup,attr" json:"lineup"`
		Device []struct {
			UUID string `xml:"uuid,attr" json:"uuid"`
			URI  string `xml:"uri,attr" json:"uri"`
		} `xml:"Device" json:"Device"`
	} `xml:"Dvr" json:"Dvr"`
	Device []struct {
		UUID string `xml:"uuid,attr" json:"uuid"`
	} `xml:"Device" json:"Device"`
}

// plexLive makes a Live TV call and reads its answer, if it has one.
func (c *Client) plexLive(ctx context.Context, s Server, method, path string, q url.Values) (plexLiveDoc, error) {
	var doc plexLiveDoc
	data, _, err := c.plexRaw(ctx, s, method, path, q, nil, "")
	if err != nil {
		return doc, explain(s, err)
	}
	data = bytes.TrimSpace(data)
	switch {
	case len(data) == 0:
		return doc, nil
	case data[0] == '<':
		if err := xml.Unmarshal(data, &doc); err != nil {
			return doc, fmt.Errorf("Plex's answer couldn't be read: %w", err)
		}
	default:
		var wrap struct {
			MediaContainer plexLiveDoc `json:"MediaContainer"`
		}
		if err := json.Unmarshal(data, &wrap); err != nil {
			return doc, fmt.Errorf("Plex's answer couldn't be read: %w", err)
		}
		doc = wrap.MediaContainer
	}
	return doc, nil
}

// PlexDVRs lists Plex's DVRs.
func (c *Client) PlexDVRs(ctx context.Context, s Server) ([]PlexDVR, error) {
	doc, err := c.plexLive(ctx, s, http.MethodGet, "/livetv/dvrs", nil)
	if err != nil {
		return nil, err
	}
	var out []PlexDVR
	for _, d := range doc.Dvr {
		dvr := PlexDVR{Key: d.Key, Lineup: d.Lineup}
		for _, dev := range d.Device {
			dvr.Devices = append(dvr.Devices, PlexTuner{UUID: dev.UUID, URI: dev.URI})
		}
		out = append(out, dvr)
	}
	return out, nil
}

// PlexAddTuner adds a network tuner by its address and says its id.
func (c *Client) PlexAddTuner(ctx context.Context, s Server, uri string) (string, error) {
	q := url.Values{}
	q.Set("uri", uri)
	doc, err := c.plexLive(ctx, s, http.MethodPost, "/media/grabbers/devices", q)
	if err != nil {
		return "", err
	}
	if len(doc.Device) == 0 || doc.Device[0].UUID == "" {
		return "", userErr(nil, "Plex didn't find a tuner at %s.", uri)
	}
	return doc.Device[0].UUID, nil
}

// PlexXMLTVLineup is how Plex names an XMLTV guide.
func PlexXMLTVLineup(xmltvURL, title string) string {
	return "lineup://tv.plex.providers.epg.xmltv/" + xmltvURL + "#" + title
}

// PlexAddDVR makes a DVR of the tuner with the guide, and says its key
// ("" when Plex didn't say: look it up with PlexDVRs).
func (c *Client) PlexAddDVR(ctx context.Context, s Server, device, lineup string) (string, error) {
	q := url.Values{}
	q.Set("language", "eng")
	q.Set("device", device)
	q.Set("lineup", lineup)
	doc, err := c.plexLive(ctx, s, http.MethodPost, "/livetv/dvrs", q)
	if err != nil {
		return "", err
	}
	if len(doc.Dvr) == 0 {
		return "", nil
	}
	return doc.Dvr[0].Key, nil
}

// PlexReloadGuide has Plex read the DVR's guide again.
func (c *Client) PlexReloadGuide(ctx context.Context, s Server, dvrKey string) error {
	_, err := c.plexLive(ctx, s, http.MethodPost, "/livetv/dvrs/"+url.PathEscape(strings.TrimPrefix(dvrKey, "/livetv/dvrs/"))+"/reloadGuide", nil)
	return err
}

// PlexDeleteDVR removes a DVR (its tuner stays known to Plex).
func (c *Client) PlexDeleteDVR(ctx context.Context, s Server, dvrKey string) error {
	_, err := c.plexLive(ctx, s, http.MethodDelete, "/livetv/dvrs/"+url.PathEscape(strings.TrimPrefix(dvrKey, "/livetv/dvrs/")), nil)
	return err
}
