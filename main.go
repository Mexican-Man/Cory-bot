package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/bwmarrin/discordgo"
	"gopkg.in/yaml.v2"
)

// Config loaded from config.yml
type Config struct {
	Bot struct {
		Token             string `yaml:"token"`
		TimeoutChannel    string `yaml:"timeoutChannel"`
		ModRoleID         string `yaml:"modRole"`
		TimeoutRoleID     string `yaml:"timeoutRole"`
		GuildID           string `yaml:"serverID"`
		PermissionAll     int64  `yaml:"permissionDenyForAllChannels"`
		PermissionTimeout int64  `yaml:"permissionAllowForTimeoutChannel"`
	} `yaml:"bot"`
}

var cfg Config

var (
	commands = []*discordgo.ApplicationCommand{
		{
			Name:        "timeout",
			Description: "Send a user on timeout. They will only be allowed to post in the timeout channel.",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionUser,
					Name:        "user",
					Description: "User to send to timeout.",
					Required:    true,
				},
			},
		},
		{
			Name:        "untimeout",
			Description: "Remove a user from timeout.",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionUser,
					Name:        "user",
					Description: "User to remove send to timeout.",
					Required:    true,
				},
			},
		},
		{
			Name:        "timeout-mods",
			Description: "Set the role for moderators, who will be able to use /timeout.",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionRole,
					Name:        "role",
					Description: "Role for moderators.",
					Required:    true,
				},
			},
		},
		{
			Name:        "timeout-role",
			Description: "Set which role is the timeout role.",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionRole,
					Name:        "role",
					Description: "Role for timeout.",
					Required:    true,
				},
			},
		},
		{
			Name:        "timeout-channel",
			Description: "Set a timeout channel.",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionChannel,
					Name:        "channel",
					Description: "Timeout channel.",
					Required:    true,
				},
			},
		},
	}
	commandHandlers = map[string]func(s *discordgo.Session, i *discordgo.InteractionCreate){
		"timeout": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			interactionHandler(s, i.Interaction, 0)
		},
		"untimeout": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			interactionHandler(s, i.Interaction, 1)
		},
		"timeout-mods": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			interactionHandler(s, i.Interaction, 2)
		},
		"timeout-role": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			interactionHandler(s, i.Interaction, 3)
		},
		"timeout-channel": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			interactionHandler(s, i.Interaction, 4)
		},
	}
)

func main() {
	// Read config
	b, err := os.ReadFile("config.yml")
	if err == os.ErrNotExist {
		os.Create("config.yml")
		cfg.Bot.PermissionAll = 34816
		updateConfigFile()
	} else if err != nil {
		log.Fatalln(err)
	}

	// Parse config
	err = yaml.Unmarshal(b, &cfg)
	if err != nil {
		log.Fatalln(err)
	}

	// Create bot, check error
	discord, err := discordgo.New(fmt.Sprintf("Bot %s", cfg.Bot.Token))
	if err != nil {
		log.Fatalln(err)
	}

	// State intents; only want to get data about messages sent
	discord.Identify.Intents = discordgo.MakeIntent(discordgo.IntentsGuildMembers | discordgo.IntentsGuilds | discordgo.IntentsGuildPresences)

	// Define handler
	discord.AddHandler(ready)

	// Open socket to discord
	err = discord.Open()
	if err != nil {
		log.Fatal(err)
	}

	// Cleanly close down the Discord session.
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, os.Kill)
	<-sc
	discord.Close()
}

func ready(s *discordgo.Session, r *discordgo.Ready) {
	// Register commands with discord
	for _, v := range commands {
		_, err := s.ApplicationCommandCreate(s.State.User.ID, cfg.Bot.GuildID, v)
		if err != nil {
			log.Panicf("Cannot create '%v' command: %v", v.Name, err)
		}
	}

	// Fix channel permissions
	setChannelPerms(s)

	// Assign handler functions
	s.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if h, ok := commandHandlers[i.ApplicationCommandData().Name]; ok {
			h(s, i)
		}
	})

	// Assign handlers to set correct permissions on channels always
	s.AddHandler(func(s *discordgo.Session, i *discordgo.ChannelUpdate) {
		setChannelPerms(s)
	})
	s.AddHandler(func(s *discordgo.Session, i *discordgo.ChannelCreate) {
		setChannelPerms(s)
	})
}

// updateConfigFile will update config.yml with the current settings
func updateConfigFile() {
	// Convert config to text
	b, err := yaml.Marshal(cfg)
	if err != nil {
		log.Fatalln(err)
	}

	os.Remove("config.yml")             // Remove old config file
	os.WriteFile("config.yml", b, 0777) // Create new config file, add config
}

// setChannelPerms will apply a permission to each channel so that the timeout role cannot speak in them
func setChannelPerms(s *discordgo.Session) {
	channels, _ := s.State.Guild(cfg.Bot.GuildID)
	for _, c := range channels.Channels {
		if c.ID == cfg.Bot.TimeoutChannel {
			s.ChannelPermissionSet(c.ID, cfg.Bot.TimeoutRoleID, discordgo.PermissionOverwriteTypeRole, cfg.Bot.PermissionTimeout, 0)
		} else {
			s.ChannelPermissionSet(c.ID, cfg.Bot.TimeoutRoleID, discordgo.PermissionOverwriteTypeRole, 0, cfg.Bot.PermissionAll)
		}
	}
}

// interactionHandler will handle everything for interactions. Kind: 0=add timeout, 1=remove timeout, 2=set mod role, 3=set timeout role, 4=set timeout channel
func interactionHandler(s *discordgo.Session, i *discordgo.Interaction, kind int) {

	// Get guild
	guild, err := s.State.Guild(i.GuildID)
	if err != nil {
		log.Println(err)
		return
	}

	// Only mods or owner can use
	if i.Member.User.ID == guild.OwnerID {
		goto AUTHORIZED
	}
	if hasRole(i.Member, &cfg.Bot.ModRoleID) {
		goto AUTHORIZED
	}

	return // Do nothing

AUTHORIZED:
	switch kind {
	case 0, 1:
		// Check if target member is found
		var member *discordgo.Member
		for _, m := range guild.Members {
			if m.User.ID == i.ApplicationCommandData().Options[0].UserValue(s).ID {
				member = m
				goto FOUND
			}
		}

		return // Do nothing

	FOUND:
		// Add/remove role
		if kind == 1 && hasRole(member, &cfg.Bot.TimeoutRoleID) {
			// Remove role
			err := s.GuildMemberRoleRemove(guild.ID, member.User.ID, cfg.Bot.TimeoutRoleID)
			if err != nil {
				log.Println(err)
				return
			}

			// Response
			s.InteractionRespond(i, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: fmt.Sprintf("<@%s> has been taken out of timeout.", i.ApplicationCommandData().Options[0].UserValue(s).ID),
				},
			})
		} else if kind == 0 && !hasRole(member, &cfg.Bot.TimeoutRoleID) {
			// Add role
			err := s.GuildMemberRoleAdd(guild.ID, member.User.ID, cfg.Bot.TimeoutRoleID)
			if err != nil {
				log.Println(err)
				return
			}

			// Response
			s.InteractionRespond(i, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: fmt.Sprintf("<@%s> has been put on timeout.", i.ApplicationCommandData().Options[0].UserValue(s).ID),
				},
			})
		}
	case 2:
		data := i.ApplicationCommandData().Options[0].RoleValue(s, guild.ID)

		// Only owner can use
		if i.Member.User.ID != guild.OwnerID {
			return
		}
		// Set new channel
		cfg.Bot.ModRoleID = data.ID
		cfg.Bot.GuildID = i.GuildID
		updateConfigFile()

		// Response
		s.InteractionRespond(i, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("<@%s> has been set as the new mod role.", data.ID),
			},
		})
	case 3:
		data := i.ApplicationCommandData().Options[0].RoleValue(s, guild.ID)

		// Only owner can use
		if i.Member.User.ID != guild.OwnerID {
			return
		}
		// Set new channel
		cfg.Bot.TimeoutRoleID = data.ID
		cfg.Bot.GuildID = i.GuildID
		updateConfigFile()
		setChannelPerms(s)

		// Response
		s.InteractionRespond(i, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("<@%s> has been set as the new timeout role.", data.ID),
			},
		})
	case 4:
		data := i.ApplicationCommandData().Options[0].ChannelValue(s)

		// Only owner can use
		if i.Member.User.ID != guild.OwnerID || data.Type != discordgo.ChannelTypeGuildText {
			return
		}

		// Defer response
		log.Println("1")
		s.InteractionRespond(i, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprint("Loading..."),
			},
		})

		// Set new channel
		cfg.Bot.TimeoutChannel = data.ID
		cfg.Bot.GuildID = i.GuildID
		updateConfigFile()
		setChannelPerms(s)

		// Response
		s.FollowupMessageCreate(s.State.User.ID, i, true, &discordgo.WebhookParams{
			Content: fmt.Sprintf("<#%s> has been set as the new timeout channel.", data.ID),
		})
	}

	return // Do nothing
}

func hasRole(user *discordgo.Member, role *string) bool {
	for _, r := range user.Roles {
		if r == *role {
			return true
		}
	}
	return false
}
