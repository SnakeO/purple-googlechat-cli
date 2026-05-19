// Package main is the entrypoint for the gchat CLI.
// gchat provides terminal access to personal Google Chat
// using the reverse-engineered Dynamite protocol.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/jacobchapa/gchat/internal/api"
	"github.com/jacobchapa/gchat/internal/auth"
	"github.com/jacobchapa/gchat/internal/channel"
	"github.com/jacobchapa/gchat/internal/config"
	"github.com/jacobchapa/gchat/internal/model"
	pb "github.com/jacobchapa/gchat/internal/proto"
	"github.com/jacobchapa/gchat/internal/transport"
	"github.com/spf13/cobra"
)

var jsonOutput bool

func main() {
	if err := rootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

// rootCmd builds the root command tree.
func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "gchat",
		Short: "Google Chat CLI for personal accounts",
	}
	root.PersistentFlags().BoolVar(&jsonOutput, "json", false, "output as JSON")

	root.AddCommand(authCmd())
	root.AddCommand(conversationsCmd())
	root.AddCommand(messagesCmd())
	root.AddCommand(sendCmd())
	root.AddCommand(whoamiCmd())
	root.AddCommand(recentCmd())
	root.AddCommand(dmsCmd())
	root.AddCommand(watchCmd())

	return root
}

// --- Auth Commands ---

// authCmd groups authentication subcommands.
func authCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage authentication",
	}
	cmd.AddCommand(authLoginCmd())
	cmd.AddCommand(authStatusCmd())
	cmd.AddCommand(authLogoutCmd())
	return cmd
}

// authLoginCmd handles the `gchat auth login` command.
func authLoginCmd() *cobra.Command {
	var useCookies bool
	var useOAuth bool
	var useBrowser bool
	var clientID string
	var clientSecret string
	var profile string

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with Google Chat",
		RunE: func(cmd *cobra.Command, args []string) error {
			if useOAuth {
				return loginOAuth(clientID, clientSecret)
			}
			if useBrowser {
				return loginBrowser(profile)
			}
			return loginCookies()
		},
	}
	cmd.Flags().BoolVar(&useCookies, "cookies", false, "use cookie-based auth (paste cookies)")
	cmd.Flags().BoolVar(&useOAuth, "oauth", false, "use OAuth auth")
	cmd.Flags().BoolVar(&useBrowser, "browser", false, "extract cookies from Chrome automatically")
	cmd.Flags().StringVar(&profile, "profile", "", "Chrome profile name (e.g. 'Profile 3')")
	cmd.Flags().StringVar(&clientID, "client-id", "", "custom OAuth client ID")
	cmd.Flags().StringVar(&clientSecret, "client-secret", "", "custom OAuth client secret")
	return cmd
}

// loginCookies performs cookie-based authentication via manual paste.
func loginCookies() error {
	fmt.Println("Enter cookies from chat.google.com (one per line, format: NAME=VALUE)")
	fmt.Println("Required: COMPASS, SSID, SID, OSID, HSID")
	fmt.Println("Optional: SAPISID")
	fmt.Println("Enter a blank line when done.")

	cookies := make(map[string]string)
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			break
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		cookies[parts[0]] = parts[1]
	}

	session, err := auth.NewCookieSession(cookies)
	if err != nil {
		return err
	}

	fmt.Println("Bootstrapping XSRF token...")
	if err := session.BootstrapXSRF(&http.Client{}); err != nil {
		return fmt.Errorf("XSRF bootstrap failed: %w", err)
	}

	creds := &config.Credentials{
		Method:  "cookie",
		Cookies: cookies,
		XSRF:    session.XSRF(),
	}

	if err := config.SaveCredentials(creds); err != nil {
		return err
	}

	fmt.Println("Authenticated successfully via cookies.")
	return nil
}

// loginOAuth performs OAuth-based authentication.
func loginOAuth(clientID, clientSecret string) error {
	if clientID != "" {
		auth.SetOAuthCredentials(clientID, clientSecret)
	}
	authURL := auth.AuthorizationURL()
	fmt.Println("Opening browser for Google login...")
	fmt.Println(authURL)
	fmt.Println("\nWaiting for authorization callback on localhost:8855...")

	openBrowser(authURL)

	code, err := auth.WaitForAuthCode()
	if err != nil {
		return err
	}

	fmt.Println("Got authorization code, exchanging for tokens...")
	session, err := auth.NewOAuthSessionFromCode(code, &http.Client{})
	if err != nil {
		return err
	}

	creds := &config.Credentials{
		Method:         "oauth",
		RefreshToken:   session.RefreshToken(),
		AccessToken:    session.AccessToken(),
		OAuthClientID:  auth.OAuthClientID,
		OAuthClientSec: auth.OAuthClientSecret,
	}

	if err := config.SaveCredentials(creds); err != nil {
		return err
	}

	fmt.Println("Authenticated successfully via OAuth.")
	return nil
}

// loginBrowser extracts fresh cookies directly from Chrome's cookie store.
func loginBrowser(profile string) error {
	if profile == "" {
		profiles, err := auth.FindChromeProfiles()
		if err != nil {
			return err
		}
		if len(profiles) == 0 {
			return fmt.Errorf("no Chrome profiles with chat.google.com cookies found")
		}
		if len(profiles) == 1 {
			profile = profiles[0].Name
			fmt.Printf("Using Chrome profile: %s\n", profile)
		} else {
			fmt.Println("Multiple Chrome profiles found with Google Chat cookies:")
			for i, p := range profiles {
				fmt.Printf("  [%d] %s\n", i+1, p.Name)
			}
			fmt.Print("Select profile number: ")
			scanner := bufio.NewScanner(os.Stdin)
			scanner.Scan()
			var idx int
			fmt.Sscanf(strings.TrimSpace(scanner.Text()), "%d", &idx)
			if idx < 1 || idx > len(profiles) {
				return fmt.Errorf("invalid selection")
			}
			profile = profiles[idx-1].Name
		}
	}

	chromeDir := ""
	profiles, _ := auth.FindChromeProfiles()
	for _, p := range profiles {
		if p.Name == profile {
			chromeDir = p.Path
			break
		}
	}
	if chromeDir == "" {
		return fmt.Errorf("Chrome profile %q not found", profile)
	}

	fmt.Printf("Extracting cookies from Chrome %s...\n", profile)
	cookies, err := auth.ExtractChromeGoogleCookies(chromeDir)
	if err != nil {
		return fmt.Errorf("cookie extraction failed: %w", err)
	}

	fmt.Printf("Got %d cookies\n", len(cookies))

	session, err := auth.NewCookieSession(cookies)
	if err != nil {
		return err
	}

	fmt.Println("Bootstrapping XSRF token...")
	if err := session.BootstrapXSRF(&http.Client{}); err != nil {
		return fmt.Errorf("XSRF bootstrap failed: %w", err)
	}

	creds := &config.Credentials{
		Method:  "cookie",
		Cookies: cookies,
		XSRF:    session.XSRF(),
	}

	if err := config.SaveCredentials(creds); err != nil {
		return err
	}

	fmt.Println("Authenticated successfully via Chrome cookies.")
	return nil
}

// authStatusCmd shows current auth state.
func authStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show authentication status",
		RunE: func(cmd *cobra.Command, args []string) error {
			creds, err := config.LoadCredentials()
			if err != nil {
				return err
			}
			if creds == nil {
				fmt.Println("Not authenticated. Run: gchat auth login")
				return nil
			}
			fmt.Printf("Method: %s\n", creds.Method)
			if creds.SelfGaiaID != "" {
				fmt.Printf("Gaia ID: %s\n", creds.SelfGaiaID)
			}
			return nil
		},
	}
}

// authLogoutCmd clears stored credentials.
func authLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Clear stored credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := config.DeleteCredentials(); err != nil {
				return err
			}
			fmt.Println("Logged out.")
			return nil
		},
	}
}

// --- API Commands ---

// loadSession creates an authenticated session from stored credentials.
func loadSession() (auth.Session, error) {
	creds, err := config.LoadCredentials()
	if err != nil {
		return nil, err
	}
	if creds == nil {
		return nil, fmt.Errorf("not authenticated — run: gchat auth login")
	}

	switch creds.Method {
	case "cookie":
		session, err := auth.NewCookieSession(creds.Cookies)
		if err != nil {
			return nil, err
		}
		if creds.XSRF != "" {
			session.SetXSRF(creds.XSRF)
		} else {
			fmt.Fprintf(os.Stderr, "Bootstrapping XSRF token...\n")
			if err := session.BootstrapXSRF(&http.Client{}); err != nil {
				return nil, fmt.Errorf("XSRF bootstrap failed: %w", err)
			}
			creds.XSRF = session.XSRF()
			config.SaveCredentials(creds)
		}
		return session, nil

	case "oauth":
		if creds.OAuthClientID != "" {
			auth.SetOAuthCredentials(creds.OAuthClientID, creds.OAuthClientSec)
		}
		return auth.NewOAuthSessionFromRefreshToken(creds.RefreshToken, &http.Client{})

	default:
		return nil, fmt.Errorf("unknown auth method: %s", creds.Method)
	}
}

// conversationsCmd lists conversations.
func conversationsCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "conversations",
		Aliases: []string{"convos", "ls"},
		Short:   "List conversations",
		RunE: func(cmd *cobra.Command, args []string) error {
			session, err := loadSession()
			if err != nil {
				return err
			}

			client := transport.NewClient(session)
			chatAPI := api.New(client)
			resp, err := chatAPI.PaginatedWorld(api.NewRequestHeader())
			if err != nil {
				return err
			}

			convos := make([]model.Conversation, 0, len(resp.GetWorldItems()))
			for _, item := range resp.GetWorldItems() {
				convos = append(convos, model.ConversationFromWorldItem(item))
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(convos)
			}

			for _, c := range convos {
				typeStr := "Space"
				if c.IsDM {
					typeStr = "DM"
				}
				lastTime := ""
				if !c.LastTime.IsZero() {
					lastTime = c.LastTime.Format(time.RFC3339)
				}
				fmt.Printf("[%s] %-5s %-30s %s\n", c.ID, typeStr, c.Name, lastTime)
				if c.LastMsg != "" {
					fmt.Printf("  > %s\n", c.LastMsg)
				}
			}
			return nil
		},
	}
}

// messagesCmd fetches message history for a conversation.
func messagesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "messages <conversation_id>",
		Short: "Read messages from a conversation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			groupID, err := model.ParseGroupID(args[0])
			if err != nil {
				return err
			}

			session, err := loadSession()
			if err != nil {
				return err
			}

			client := transport.NewClient(session)
			chatAPI := api.New(client)

			fromTS := int64(0)
			resp, err := chatAPI.CatchUpGroup(api.NewRequestHeader(), groupID, fromTS)
			if err != nil {
				return err
			}

			var messages []model.Message
			for _, event := range resp.GetEvents() {
				for _, msg := range extractMessages(event) {
					messages = append(messages, model.MessageFromProto(msg))
				}
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(messages)
			}

			for _, m := range messages {
				timeStr := m.Time.Format("15:04")
				sender := m.Sender
				if sender == "" {
					sender = m.SenderID
				}
				fmt.Printf("[%s] %s: %s\n", timeStr, sender, m.Text)
			}
			return nil
		},
	}
}

// sendCmd sends a message to a conversation.
func sendCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "send <conversation_id> <message>",
		Short: "Send a message",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			groupID, err := model.ParseGroupID(args[0])
			if err != nil {
				return err
			}

			session, err := loadSession()
			if err != nil {
				return err
			}

			client := transport.NewClient(session)
			chatAPI := api.New(client)

			localID := fmt.Sprintf("gchat_%d", time.Now().UnixNano())
			_, err = chatAPI.CreateTopic(api.NewRequestHeader(), groupID, args[1], localID)
			if err != nil {
				return err
			}

			fmt.Println("Message sent.")
			return nil
		},
	}
}

// whoamiCmd shows the authenticated user's identity.
func whoamiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show authenticated user info",
		RunE: func(cmd *cobra.Command, args []string) error {
			session, err := loadSession()
			if err != nil {
				return err
			}

			client := transport.NewClient(session)
			chatAPI := api.New(client)
			resp, err := chatAPI.GetSelfUserStatus(api.NewRequestHeader())
			if err != nil {
				return err
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(resp)
			}

			us := resp.GetUserStatus()
			fmt.Printf("Gaia ID: %s\n", us.GetUserId().GetId())
			return nil
		},
	}
}

// recentCmd shows all recent messages across all conversations.
func recentCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "recent",
		Short: "Show recent messages across all conversations",
		RunE: func(cmd *cobra.Command, args []string) error {
			session, err := loadSession()
			if err != nil {
				return err
			}

			client := transport.NewClient(session)
			chatAPI := api.New(client)

			fromTS := time.Now().Add(-24 * time.Hour).UnixMicro()
			resp, err := chatAPI.CatchUpUser(api.NewRequestHeader(), fromTS)
			if err != nil {
				return err
			}

			for _, event := range resp.GetEvents() {
				for _, msg := range extractMessages(event) {
					m := model.MessageFromProto(msg)
					gid := model.FormatGroupID(event.GetGroupId())
					sender := m.Sender
					if sender == "" {
						sender = m.SenderID
					}
					fmt.Printf("[%s] %s | %s: %s\n", m.Time.Format("15:04"), gid, sender, m.Text)
				}
			}

			return nil
		},
	}
}

// dmsCmd lists all DM contacts with resolved names.
func dmsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "dms",
		Short: "List all DM contacts with names",
		RunE: func(cmd *cobra.Command, args []string) error {
			session, err := loadSession()
			if err != nil {
				return err
			}

			client := transport.NewClient(session)
			chatAPI := api.New(client)

			resp, err := chatAPI.PaginatedWorld(api.NewRequestHeader())
			if err != nil {
				return err
			}

			selfResp, err := chatAPI.GetSelfUserStatus(api.NewRequestHeader())
			if err != nil {
				return fmt.Errorf("cannot get self ID: %w", err)
			}
			selfID := selfResp.GetUserStatus().GetUserId().GetId()

			type dmContact struct {
				DMID string `json:"dm_id"`
				Name string `json:"name"`
				UID  string `json:"uid"`
			}
			var contacts []dmContact

			for _, item := range resp.GetWorldItems() {
				gid := item.GetGroupId()
				if gid.GetDmId() == nil {
					continue
				}
				dmID := gid.GetDmId().GetDmId()

				members, err := chatAPI.ListMembers(api.NewRequestHeader(), gid)
				if err != nil {
					continue
				}

				for _, ms := range members.GetMemberships() {
					mid := ms.GetId().GetMemberId()
					uid := mid.GetUserId().GetId()
					if uid == selfID || uid == "" {
						continue
					}

					name := ""
					memberResp, err := chatAPI.GetMembers(api.NewRequestHeader(), []*pb.MemberId{mid})
					if err == nil {
						for _, m := range memberResp.GetMembers() {
							if u := m.GetUser(); u != nil {
								name = u.GetName()
								break
							}
						}
					}
					if name == "" {
						name = "user:" + uid
					}

					contacts = append(contacts, dmContact{DMID: dmID, Name: name, UID: uid})
				}
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(contacts)
			}

			for _, c := range contacts {
				fmt.Printf("dm:%-15s %s\n", c.DMID, c.Name)
			}

			return nil
		},
	}
}

// watchCmd streams real-time events from the webchannel.
func watchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "watch",
		Short: "Stream real-time messages",
		RunE: func(cmd *cobra.Command, args []string) error {
			session, err := loadSession()
			if err != nil {
				return err
			}

			fmt.Println("Connecting to webchannel...")

			conn := channel.NewConnection(session, func(evt channel.Event) {
				if evt.Proto == nil {
					return
				}

				event := evt.Proto.GetEvent()
				if event == nil {
					return
				}

				for _, msg := range extractMessages(event) {
					m := model.MessageFromProto(msg)
					gid := model.FormatGroupID(event.GetGroupId())
					sender := m.Sender
					if sender == "" {
						sender = m.SenderID
					}
					if jsonOutput {
						data, _ := json.Marshal(m)
						fmt.Println(string(data))
					} else {
						fmt.Printf("[%s] %s | %s: %s\n", m.Time.Format("15:04"), gid, sender, m.Text)
					}
				}
			})

			return conn.Connect()
		},
	}
}

// extractMessages pulls Message objects from both body and bodies fields of an event.
func extractMessages(event *pb.Event) []*pb.Message {
	var msgs []*pb.Message

	if b := event.GetBody(); b != nil {
		if mp := b.GetMessagePosted(); mp != nil && mp.GetMessage() != nil {
			msgs = append(msgs, mp.GetMessage())
		}
	}

	for _, b := range event.GetBodies() {
		if mp := b.GetMessagePosted(); mp != nil && mp.GetMessage() != nil {
			msgs = append(msgs, mp.GetMessage())
		}
	}

	return msgs
}

// openBrowser tries to open a URL in the default browser.
func openBrowser(url string) {
	exec.Command("open", url).Start()
}
