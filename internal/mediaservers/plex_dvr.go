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
		Key  string `xml:"key,attr" json:"key"`
	} `xml:"Device" json:"Device"`
	// A tuner's channels (/media/grabbers/devices/{id}/channels).
	DeviceChannel []struct {
		Identifier string `xml:"identifier,attr" json:"identifier"`
		Key        string `xml:"key,attr" json:"key"`
		Name       string `xml:"name,attr" json:"name"`
	} `xml:"DeviceChannel" json:"DeviceChannel"`
	// A guide's channels (/livetv/epg/lineupchannels).
	Channel []struct {
		Identifier string `xml:"identifier,attr" json:"identifier"`
		Key        string `xml:"key,attr" json:"key"`
		VCN        string `xml:"channelVcn,attr" json:"channelVcn"`
		Title      string `xml:"title,attr" json:"title"`
	} `xml:"Channel" json:"Channel"`
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

// PlexTunerKey is a tuner's own key (its number in /media/grabbers/devices),
// from its id.
func (c *Client) PlexTunerKey(ctx context.Context, s Server, uuid string) (string, error) {
	doc, err := c.plexLive(ctx, s, http.MethodGet, "/media/grabbers/devices", nil)
	if err != nil {
		return "", err
	}
	for _, d := range doc.Device {
		if d.UUID == uuid {
			return strings.TrimPrefix(d.Key, "/media/grabbers/devices/"), nil
		}
	}
	return "", userErr(nil, "Plex doesn't list the tuner %s.", uuid)
}

// PlexMapChannels links each of the tuner's channels to the guide's channel
// of the same number and switches them on, as Plex's own setup does. It
// says how many it switched on.
func (c *Client) PlexMapChannels(ctx context.Context, s Server, tunerKey, lineup string) (int, error) {
	dev, err := c.plexLive(ctx, s, http.MethodGet, "/media/grabbers/devices/"+url.PathEscape(tunerKey)+"/channels", nil)
	if err != nil {
		return 0, err
	}
	q := url.Values{}
	q.Set("lineup", lineup)
	guide, err := c.plexLive(ctx, s, http.MethodGet, "/livetv/epg/lineupchannels", q)
	if err != nil {
		return 0, err
	}
	type gc struct{ vcn, key string }
	byNumber := map[string]gc{}
	for _, ch := range guide.Channel {
		g := gc{ch.VCN, ch.Key}
		if g.vcn == "" {
			g.vcn = ch.Identifier
		}
		for _, k := range []string{ch.VCN, ch.Identifier} {
			if k != "" {
				byNumber[k] = g
			}
		}
	}
	m := url.Values{}
	var on []string
	for _, ch := range dev.DeviceChannel {
		id := ch.Identifier
		if id == "" {
			id = ch.Key
		}
		g, ok := byNumber[id]
		if !ok || id == "" {
			continue
		}
		m.Set("channelMapping["+id+"]", g.vcn)
		if g.key != "" {
			m.Set("channelMappingByKey["+id+"]", g.key)
		}
		on = append(on, id)
	}
	if len(on) == 0 {
		return 0, userErr(nil, "none of the tuner's %d channels matched the guide's %d", len(dev.DeviceChannel), len(guide.Channel))
	}
	m.Set("channelsEnabled", strings.Join(on, ","))
	if _, err := c.plexLive(ctx, s, http.MethodPut, "/media/grabbers/devices/"+url.PathEscape(tunerKey)+"/channelmap", m); err != nil {
		return 0, err
	}
	return len(on), nil
}
