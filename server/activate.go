package main

import (
	"github.com/mattermost/mattermost-server/v5/model"
	"io/ioutil"
	"path/filepath"
)

const minimumServerVersion = "5.10.0"
const botName = "rssfeedbot"
const botDisplayName = "RSSFeed Plugin"

// RssFeedIconURL ...
const RssFeedIconURL = "https://mattermost.gridprotectionalliance.org/plugins/rssfeed/images/rss.png"

// OnActivate ...
func (p *RSSFeedPlugin) OnActivate() error {
	_, err := p.ensureBotExists()
	if err != nil {
		p.API.LogError("Failed to find "+botDisplayName+" user", "err", err)
		return err
	}

	p.API.RegisterCommand(getCommand())
	p.processHeartBeatFlag = true
	go p.setupHeartBeat()

	return nil
}

// OnDeactivate ...
func (p *RSSFeedPlugin) OnDeactivate() error {

	p.processHeartBeatFlag = false
	return nil
}

func (p *RSSFeedPlugin) ensureBotExists() (string, *model.AppError) {
	p.API.LogDebug("Ensuring " + botDisplayName + " exists")

	bot, createErr := p.API.CreateBot(&model.Bot{
		Username:    botName,
		DisplayName: botDisplayName,
		Description: "Sets and triggers reminders",
	})
	if createErr != nil {
		p.API.LogError("Failed to create "+botDisplayName+". Attempting to find existing one.", "err", createErr)

		// Unable to create the bot, so it should already exist
		user, err := p.API.GetUserByUsername(botName)
		if err != nil || user == nil {
			p.API.LogError("Failed to find "+botDisplayName+" user", "err", err)
			return "", err
		}

		bot, err = p.API.GetBot(user.Id, true)
		if err != nil {
			p.API.LogError("Failed to find "+botDisplayName, "err", err)
			return "", err
		}

		p.API.LogDebug("Found " + botDisplayName)
	} else {
		if err := p.setBotProfileImage(bot.UserId); err != nil {
			p.API.LogError("Failed to set profile image for bot", "err", err)
		}

		p.API.LogDebug(botDisplayName + " created")
	}

	p.botUserID = bot.UserId

	return bot.UserId, nil
}

func (p *RSSFeedPlugin) setBotProfileImage(botUserID string) *model.AppError {
	p.API.LogDebug("Setting profile image for " + botDisplayName)

	directory := *p.API.GetConfig().PluginSettings.Directory
	p.API.LogDebug("Directory: " + directory)

	bundlePath, err := p.API.GetBundlePath()
	if err != nil {
		p.API.LogError("Failed getting bundle path for " + botDisplayName + ". " + err.Error())

		return &model.AppError{Message: err.Error()}
	}

	//profileImage, err := p.readFile(filepath.Join(bundlePath, "assets", "rss.png"))
	path := filepath.Join(bundlePath, "assets", "rss.png")
	p.API.LogDebug("Path: " + bundlePath)

	profileImage, err := ioutil.ReadFile(path)
	if err != nil {
		p.API.LogError("Failed reading file path for " + botDisplayName + ". " + err.Error())
		return &model.AppError{Message: err.Error()}
	}

	return p.API.SetProfileImage(botUserID, profileImage)
}
