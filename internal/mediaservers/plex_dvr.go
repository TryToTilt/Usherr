package mediaservers

// Usherr: Live TV in Plex. Plex's own Live TV & DVR (Plex Pass) takes a
// network tuner (an HDHomeRun, or something answering as one) and a guide.
// These are the calls Plex's own setup makes: add the tuner by its address,
// make a DVR of it with an XMLTV guide, and load the guide.

import (
	"context"
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

// PlexDVRs lists Plex's DVRs.
func (c *Client) PlexDVRs(ctx context.Context, s Server) ([]PlexDVR, error) {
	var doc struct {
		MediaContainer struct {
			Dvr []struct {
				Key    string `json:"key"`
				Lineup string `json:"lineup"`
				Device []struct {
					UUID string `json:"uuid"`
					URI  string `json:"uri"`
				} `json:"Device"`
			} `json:"Dvr"`
		} `json:"MediaContainer"`
	}
	if err := c.do(ctx, s, http.MethodGet, "/livetv/dvrs", nil, nil, &doc); err != nil {
		return nil, explain(s, err)
	}
	var out []PlexDVR
	for _, d := range doc.MediaContainer.Dvr {
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
	var doc struct {
		MediaContainer struct {
			Device []struct {
				UUID string `json:"uuid"`
			} `json:"Device"`
		} `json:"MediaContainer"`
	}
	q := url.Values{}
	q.Set("uri", uri)
	if err := c.do(ctx, s, http.MethodPost, "/media/grabbers/devices", q, nil, &doc); err != nil {
		return "", explain(s, err)
	}
	if len(doc.MediaContainer.Device) == 0 || doc.MediaContainer.Device[0].UUID == "" {
		return "", userErr(nil, "Plex didn't find a tuner at %s.", uri)
	}
	return doc.MediaContainer.Device[0].UUID, nil
}

// PlexXMLTVLineup is how Plex names an XMLTV guide.
func PlexXMLTVLineup(xmltvURL, title string) string {
	return "lineup://tv.plex.providers.epg.xmltv/" + xmltvURL + "#" + title
}

// PlexAddDVR makes a DVR of the tuner with the guide, and says its key.
func (c *Client) PlexAddDVR(ctx context.Context, s Server, device, lineup string) (string, error) {
	var doc struct {
		MediaContainer struct {
			Dvr []struct {
				Key string `json:"key"`
			} `json:"Dvr"`
		} `json:"MediaContainer"`
	}
	q := url.Values{}
	q.Set("language", "eng")
	q.Set("device", device)
	q.Set("lineup", lineup)
	if err := c.do(ctx, s, http.MethodPost, "/livetv/dvrs", q, nil, &doc); err != nil {
		return "", explain(s, err)
	}
	if len(doc.MediaContainer.Dvr) == 0 {
		return "", nil
	}
	return doc.MediaContainer.Dvr[0].Key, nil
}

// PlexReloadGuide has Plex read the DVR's guide again.
func (c *Client) PlexReloadGuide(ctx context.Context, s Server, dvrKey string) error {
	if err := c.do(ctx, s, http.MethodPost, "/livetv/dvrs/"+url.PathEscape(strings.TrimPrefix(dvrKey, "/livetv/dvrs/"))+"/reloadGuide", nil, nil, nil); err != nil {
		return explain(s, err)
	}
	return nil
}

// PlexDeleteDVR removes a DVR (its tuner stays known to Plex).
func (c *Client) PlexDeleteDVR(ctx context.Context, s Server, dvrKey string) error {
	if err := c.do(ctx, s, http.MethodDelete, "/livetv/dvrs/"+url.PathEscape(strings.TrimPrefix(dvrKey, "/livetv/dvrs/")), nil, nil, nil); err != nil {
		return explain(s, err)
	}
	return nil
}
