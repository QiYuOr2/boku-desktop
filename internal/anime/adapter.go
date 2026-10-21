package anime

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"boku/internal/domain"
)

type collectionPage struct {
	Total int `json:"total"`
	Data  []struct {
		SubjectID int64 `json:"subject_id"`
		Subject   struct {
			Name   string `json:"name"`
			NameCN string `json:"name_cn"`
			Images struct {
				Grid string `json:"grid"`
			} `json:"images"`
		} `json:"subject"`
	} `json:"data"`
}
type episodePage struct {
	Data []struct {
		ID      int64   `json:"id"`
		Type    int     `json:"type"`
		Sort    float64 `json:"sort"`
		Name    string  `json:"name"`
		NameCN  string  `json:"name_cn"`
		AirDate string  `json:"airdate"`
	} `json:"data"`
}

func collections(payload []byte) (collectionPage, error) {
	var out collectionPage
	if err := json.Unmarshal(payload, &out); err != nil {
		return out, bad("collections are not an object", payload)
	}
	if out.Total < 0 {
		return out, bad("collections total is invalid", payload)
	}
	return out, nil
}

func releases(subjectID int64, subjectName, coverURL string, payload []byte, start, end time.Time) ([]domain.EpisodeRelease, error) {
	var page episodePage
	if err := json.Unmarshal(payload, &page); err != nil {
		return nil, bad("episodes are not an object", payload)
	}
	items := make([]domain.EpisodeRelease, 0)
	for _, ep := range page.Data {
		if ep.ID <= 0 || ep.Type != 0 {
			continue
		}
		date, err := time.ParseInLocation(time.DateOnly, strings.TrimSpace(ep.AirDate), start.Location())
		if err != nil {
			if strings.TrimSpace(ep.AirDate) == "" {
				continue
			}
			return nil, bad("episode airdate is invalid", payload)
		}
		// The provider only gives a calendar date. Treat it as a Shanghai
		// calendar day; do not invent an unavailable release clock time.
		if date.Before(start) || !date.Before(end) {
			continue
		}
		name := strings.TrimSpace(ep.NameCN)
		if name == "" {
			name = strings.TrimSpace(ep.Name)
		}
		// An announced episode may not have a title yet. Keep its schedule;
		// presentation can identify it by its episode number alone.
		items = append(items, domain.EpisodeRelease{SubjectID: subjectID, SubjectName: subjectName, CoverURL: coverURL, EpisodeID: ep.ID, EpisodeName: name, EpisodeSort: ep.Sort, AirDate: date.UTC()})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].SubjectName == items[j].SubjectName {
			return items[i].EpisodeSort < items[j].EpisodeSort
		}
		return items[i].SubjectName < items[j].SubjectName
	})
	return items, nil
}

func subjectCover(c collectionPage, subjectID int64) string {
	for _, item := range c.Data {
		if item.SubjectID == subjectID {
			return strings.TrimSpace(item.Subject.Images.Grid)
		}
	}
	return ""
}
func subjectName(c collectionPage, subjectID int64) string {
	for _, item := range c.Data {
		if item.SubjectID == subjectID {
			if name := strings.TrimSpace(item.Subject.NameCN); name != "" {
				return name
			}
			return strings.TrimSpace(item.Subject.Name)
		}
	}
	return ""
}
func bad(reason string, payload []byte) error {
	return &domain.BadResponseError{ProviderID: "bangumi", Reason: reason, Snippet: domain.Snippet(payload)}
}
