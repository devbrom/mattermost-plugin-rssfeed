package main

import (
	"errors"
	"github.com/lunny/html2md"
	"github.com/mattermost/mattermost-server/v5/model"
	"github.com/mattermost/mattermost-server/v5/plugin"
	"github.com/wbernest/atom-parser"
	"github.com/wbernest/rss-v2-parser"
	"io/ioutil"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

//const RSSFEED_ICON_URL = "./plugins/rssfeed/assets/rss.png"

// RSSFeedPlugin Object
type RSSFeedPlugin struct {
	plugin.MattermostPlugin

	// configurationLock synchronizes access to the configuration.
	configurationLock sync.RWMutex

	// configuration is the active plugin configuration. Consult getConfiguration and
	// setConfiguration for usage.
	configuration *configuration

	botUserID            string
	processHeartBeatFlag bool
}

// ServeHTTP hook from mattermost plugin
func (p *RSSFeedPlugin) ServeHTTP(c *plugin.Context, w http.ResponseWriter, r *http.Request) {
	switch path := r.URL.Path; path {
	case "/images/rss.png":
		data, err := ioutil.ReadFile(string("plugins/rssfeed/assets/rss.png"))
		if err == nil {
			w.Header().Set("Content-Type", "image/png")
			w.Write(data)
		} else {
			w.WriteHeader(404)
			w.Write([]byte("404 Something went wrong - " + http.StatusText(404)))
			p.API.LogInfo("/images/rss.png err = ", err.Error())
		}
	default:
		w.Header().Set("Content-Type", "application/json")
		http.NotFound(w, r)
	}
}

func (p *RSSFeedPlugin) setupHeartBeat() {
	heartbeatTime, err := p.getHeartbeatTime()
	if err != nil {
		p.API.LogError(err.Error())
	}

	for p.processHeartBeatFlag {
		//p.API.LogDebug("Heartbeat")

		err := p.processHeartBeat()
		if err != nil {
			p.API.LogError(err.Error())

		}
		time.Sleep(time.Duration(heartbeatTime) * time.Minute)
	}
}

func (p *RSSFeedPlugin) processHeartBeat() error {
	dictionaryOfSubscriptions, err := p.getSubscriptions()
	if err != nil {
		return err
	}

	for _, value := range dictionaryOfSubscriptions.Subscriptions {
		err := p.processSubscription(value)
		if err != nil {
			p.API.LogError(err.Error())
		}
	}

	return nil
}

func (p *RSSFeedPlugin) getHeartbeatTime() (int, error) {
	config := p.getConfiguration()
	heartbeatTime := 15
	var err error
	if len(config.Heartbeat) > 0 {
		heartbeatTime, err = strconv.Atoi(config.Heartbeat)
		if err != nil {
			return 15, err
		}
	}

	return heartbeatTime, nil
}

func (p *RSSFeedPlugin) processSubscription(subscription *Subscription) error {

	if len(subscription.URL) == 0 {
		return errors.New("no url supplied")
	}

	if rssv2parser.IsValidFeed(subscription.URL) {
		err := p.processRSSV2Subscription(subscription)
		if err != nil {
			return errors.New("invalid RSS v2 feed format - " + err.Error())
		}

	} else if atomparser.IsValidFeed(subscription.URL) {
		err := p.processAtomSubscription(subscription)
		if err != nil {
			return errors.New("invalid atom feed format - " + err.Error())
		}
	} else {
		return errors.New("invalid feed format")
	}

	return nil
}

func (p *RSSFeedPlugin) processRSSV2Subscription(subscription *Subscription) error {
	config := p.getConfiguration()

	// get new rss feed string from url
	newRssFeed, newRssFeedString, err := rssv2parser.ParseURL(subscription.URL)
	if err != nil {
		return err
	}

	// retrieve old xml feed from database
	oldRssFeed, err := rssv2parser.ParseString(subscription.XML)
	if err != nil {
		return err
	}

	items := rssv2parser.CompareItemsBetweenOldAndNew(oldRssFeed, newRssFeed)
	fieldsRegexp := regexp.MustCompile(`(?m:^\*\*(.+)\*\*:\s?(.+)$)`)
	spacesRegexp := regexp.MustCompile(`\s+`)

	for _, item := range items {
		attachment := &model.SlackAttachment{
			Title:     item.Title,
			TitleLink: item.Link,
		}

		if item.Description != "" && config.ShowDescription {
			body := item.Description
			body = html2md.Convert(body)

			titleLink, err := url.Parse(attachment.TitleLink)
			if err == nil {
				parts := strings.Split(titleLink.Hostname(), ".")
				domain := parts[len(parts)-2] + "." + parts[len(parts)-1]

				if domain == "upwork.com" {
					var fields = map[string]string{
						"Skills":   "",
						"Category": "",
						"Country":  "",
						"Budget":   "",
					}
					fieldsMatchResult := fieldsRegexp.FindAllStringSubmatch(body, -1)
					for _, fieldMatch := range fieldsMatchResult {
						body = strings.Replace(body, fieldMatch[0], "", -1)
						fields[fieldMatch[1]] = fieldMatch[2]
					}

					if fields["Budget"] == "" {
						fields["Budget"] = "Hourly"
					} else {
						fields["Budget"] = fields["Budget"] + " Fixed-price"
					}

					fields["Skills"] = spacesRegexp.ReplaceAllString(fields["Skills"], " ")

					for _, title := range []string{"Skills", "Category", "Country", "Budget"} {
						attachment.Fields = append(attachment.Fields, &model.SlackAttachmentField{
							Title: title,
							Value: fields[title],
							Short: true,
						})
					}
				}
			}

			attachment.Text = strings.TrimSpace(body)
		}
		p.createBotPost(subscription.ChannelID, attachment, "custom_git_pr")
	}

	if len(items) > 0 {
		subscription.XML = newRssFeedString
		p.updateSubscription(subscription)
	}

	return nil
}

func (p *RSSFeedPlugin) processAtomSubscription(subscription *Subscription) error {
	config := p.getConfiguration()

	// get new rss feed string from url
	newFeed, newFeedString, err := atomparser.ParseURL(subscription.URL)
	if err != nil {
		return err
	}

	// retrieve old xml feed from database
	oldFeed, err := atomparser.ParseString(subscription.XML)
	if err != nil {
		return err
	}

	items := atomparser.CompareItemsBetweenOldAndNew(oldFeed, newFeed)
	fieldsRegexp := regexp.MustCompile(`(?m:^\*\*(.+)\*\*:\s?(.+)$)`)
	spacesRegexp := regexp.MustCompile(`\s+`)

	for _, item := range items {

		attachment := &model.SlackAttachment{}
		attachment.Title = item.Title

		if item.Author != nil {
			attachment.AuthorName = item.Author.Name
			attachment.AuthorLink = item.Author.URI
		}

		for _, link := range item.Link {
			if link.Rel == "alternate" {
				attachment.TitleLink = link.Href
			}
		}

		//currently not supported
		if item.Published != "" {
			attachment.Timestamp = string(item.Published)
		} else {
			attachment.Timestamp = string(item.Updated)
		}

		if item.Content != nil && config.ShowDescription {
			body := item.Content.Body
			if item.Content.Type != "text" {
				body = html2md.Convert(body)
			}

			titleLink, err := url.Parse(attachment.TitleLink)
			if err == nil {
				parts := strings.Split(titleLink.Hostname(), ".")
				domain := parts[len(parts)-2] + "." + parts[len(parts)-1]

				if domain == "upwork.com" {
					var fields = map[string]string{
						"Skills":   "",
						"Category": "",
						"Country":  "",
						"Budget":   "",
					}
					fieldsMatchResult := fieldsRegexp.FindAllStringSubmatch(body, -1)
					for _, fieldMatch := range fieldsMatchResult {
						body = strings.Replace(body, fieldMatch[0], "", -1)
						fields[fieldMatch[1]] = fieldMatch[2]
					}

					if fields["Budget"] == "" {
						fields["Budget"] = "Hourly"
					} else {
						fields["Budget"] = fields["Budget"] + " Fixed-price"
					}

					fields["Skills"] = spacesRegexp.ReplaceAllString(fields["Skills"], " ")

					for _, title := range []string{"Skills", "Category", "Country", "Budget"} {
						attachment.Fields = append(attachment.Fields, &model.SlackAttachmentField{
							Title: title,
							Value: fields[title],
							Short: true,
						})
					}
				}
			}

			attachment.Text = strings.TrimSpace(body)

		}

		p.createBotPost(subscription.ChannelID, attachment, "custom_git_pr")
	}

	if len(items) > 0 {
		subscription.XML = newFeedString
		p.updateSubscription(subscription)
	}

	return nil
}

func (p *RSSFeedPlugin) createBotPost(channelID string, attachment *model.SlackAttachment, postType string) error {
	attachments := []*model.SlackAttachment{
		attachment,
	}

	post := &model.Post{
		UserId:    p.botUserID,
		ChannelId: channelID,
		Message:   "",
		Type:      postType,
	}
	post.AddProp("attachments", attachments)

	if _, err := p.API.CreatePost(post); err != nil {
		p.API.LogError(err.Error())
		return err
	}

	return nil
}
