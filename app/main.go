package main

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"os"
	"strings"

	"cloud.google.com/go/firestore"
	"github.com/darrenmcc/dizmo"
	"google.golang.org/grpc/status"
	"grpc.go4.org/codes"
)

const (
	ISODateFmt    = "2006-01-02"
	crReleaseKind = "CloudRunReleaseNote"
)

var slackURL = mustEnv("SLACK_URL")

func main() {
	http.Handle("/", dizmo.LogMiddleware(http.HandlerFunc(do)))
	http.ListenAndServe(":"+mustEnv("PORT"), nil)
}

type Entry struct {
	Text    string `xml:",chardata"`
	Title   string `xml:"title"`
	ID      string `xml:"id"`
	Updated string `xml:"updated"`
	Link    struct {
		Text string `xml:",chardata"`
		Rel  string `xml:"rel,attr"`
		Href string `xml:"href,attr"`
	} `xml:"link"`
	Content struct {
		Text string `xml:",chardata"`
		Type string `xml:"type,attr"`
	} `xml:"content"`
}

type Feed struct {
	XMLName xml.Name `xml:"feed"`
	Text    string   `xml:",chardata"`
	Xmlns   string   `xml:"xmlns,attr"`
	ID      string   `xml:"id"`
	Title   string   `xml:"title"`
	Link    struct {
		Text string `xml:",chardata"`
		Rel  string `xml:"rel,attr"`
		Href string `xml:"href,attr"`
	} `xml:"link"`
	Author struct {
		Text string `xml:",chardata"`
		Name string `xml:"name"`
	} `xml:"author"`
	Updated string  `xml:"updated"`
	Entries []Entry `xml:"entry"`
}

func do(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	fs, err := firestore.NewClient(ctx, dizmo.GoogleProjectID())
	if err != nil {
		dizmo.Errorf(ctx, "unable to init firestore client: %s", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	resp, err := http.Get("https://cloud.google.com/feeds/run-release-notes.xml")
	if err != nil {
		dizmo.Errorf(ctx, "unable to fetch feed: %s", err)
		// return nil, err
	}
	defer resp.Body.Close()

	var feed Feed
	err = xml.NewDecoder(resp.Body).Decode(&feed)
	if err != nil {
		dizmo.Errorf(ctx, "unable to decode feed: %s", err)
		// return nil, err
	}

	latest := feed.Entries[0]

	var data Entry
	date := strings.Split(latest.ID, "#")[1]

	doc := fs.Doc(crReleaseKind + "/" + date)
	_, err = doc.Get(ctx)
	stat, _ := status.FromError(err)
	switch {
	case stat == nil:
		// version already exists
		dizmo.Infof(ctx, "no new cloud run release notes since %s", date)
	case stat.Code() == codes.NotFound:
		content := strings.ToLower(latest.Content.Text)

		var msg string
		if n := strings.Count(content, ">feature<"); n > 0 {
			msg = fmt.Sprintf("Cloud Run has %d new feature%s", n, plural(n))
		}
		if n := strings.Count(content, ">changed<"); n > 0 {
			if msg == "" {
				msg = fmt.Sprintf("Cloud Run has %d new change%s", n, plural(n))
			} else {
				msg += fmt.Sprintf(" and %d change%s", n, plural(n))
			}
		}
		if n := strings.Count(content, ">fixed<"); n > 0 {
			if msg == "" {
				msg = fmt.Sprintf("Cloud Run has %d new fix%s", n, plurale(n))
			} else {
				msg += fmt.Sprintf(" and %d fix%s", n, plurale(n))
			}
		}

		dizmo.Infof(ctx, msg)

		// _, err := s.sendgrid.Send(mail.NewSingleEmail(
		// 	s.from,
		// 	msg,
		// 	s.to,
		// 	"XXX", // just can't be empty, screw plaintext emails apparently
		// 	"https://cloud.google.com/run/docs/release-notes\n\n"+latest.Content.Text))
		// if err != nil {
		// 	dizmo.Infof(ctx, "unable to send email: %s", err)
		// 	return nil, err
		// }

		_, err = fs.Put(ctx, k, &latest)
		if err != nil {
			dizmo.Infof(ctx, "unable to write entry to datastore: %s", err)
			return nil, err
		}
		return nil, nil
	default:
		dizmo.Infof(ctx, "unable to fetch from datastore: %s", err)
		return nil, err
	}
}

func mustEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		panic(k + " not found in environment")
	}
	return v
}

func plural(n int) string {
	if n > 1 {
		return "s"
	}
	return ""
}

func plurale(n int) string {
	if n > 1 {
		return "es"
	}
	return ""
}
