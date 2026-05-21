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

	"context"

	"github.com/jacobchapa/gchat/internal/api"
	"github.com/jacobchapa/gchat/internal/auth"
	"github.com/jacobchapa/gchat/internal/cache"
	"github.com/jacobchapa/gchat/internal/channel"
	"github.com/jacobchapa/gchat/internal/config"
	"github.com/jacobchapa/gchat/internal/embed"
	"github.com/jacobchapa/gchat/internal/model"
	pb "github.com/jacobchapa/gchat/internal/proto"
	"github.com/jacobchapa/gchat/internal/transport"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
)

// Global state initialized at startup.
var (
	jsonOutput bool
	db         *cache.Cache
	embedder   *embed.Embedder
)

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
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Skip init for auth commands — they don't need cache/embedder
			if cmd.Parent() != nil && cmd.Parent().Name() == "auth" {
				return nil
			}
			if cmd.Name() == "auth" {
				return nil
			}
			return initCacheAndEmbedder()
		},
		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			if embedder != nil {
				embedder.Close()
			}
			if db != nil {
				db.Close()
			}
		},
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
	root.AddCommand(searchCmd())
	root.AddCommand(cacheCmd())
	root.AddCommand(loadCmd())
	root.AddCommand(mentionsCmd())

	return root
}

// initCacheAndEmbedder sets up the SQLite cache. Embedder is loaded lazily on first use.
func initCacheAndEmbedder() error {
	dbPath, err := config.CachePath()
	if err != nil {
		return err
	}

	db, err = cache.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: cache unavailable: %v\n", err)
		db = nil
	}

	return nil
}

// getEmbedder returns the embedder, initializing it lazily on first call.
func getEmbedder() *embed.Embedder {
	if embedder != nil {
		return embedder
	}
	var err error
	embedder, err = embed.New(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: embedder unavailable: %v\n", err)
		embedder = nil
	}
	return embedder
}

// cacheMessage upserts a message into the cache (text only, no embedding).
// Embeddings are generated during `load` (batch) and `search` (query).
func cacheMessage(convID, msgID, senderID, text string, createdAt int64, isDeleted bool) {
	if db == nil {
		return
	}
	if err := db.UpsertMessage(convID, msgID, senderID, text, createdAt, isDeleted); err != nil {
		fmt.Fprintf(os.Stderr, "cache: message upsert: %v\n", err)
	}
}

// embedCachedMessages generates embeddings for all unembedded messages in the cache.
func embedCachedMessages() {
	if db == nil {
		return
	}

	e := getEmbedder()
	if e == nil {
		return
	}

	rows, err := db.DB().Query(`
		SELECT m.rowid, m.text FROM messages m
		LEFT JOIN vec_messages v ON m.rowid = v.rowid
		WHERE v.rowid IS NULL AND m.text != '' AND m.is_deleted = 0`)
	if err != nil {
		return
	}
	defer rows.Close()

	type pending struct {
		rowid int64
		text  string
	}
	var items []pending
	for rows.Next() {
		var p pending
		rows.Scan(&p.rowid, &p.text)
		items = append(items, p)
	}

	if len(items) == 0 {
		return
	}

	fmt.Fprintf(os.Stderr, "Embedding %d messages...\n", len(items))
	bar := progressbar.NewOptions(len(items),
		progressbar.OptionSetDescription("  Embed"),
		progressbar.OptionSetWriter(os.Stderr),
		progressbar.OptionShowCount(),
		progressbar.OptionClearOnFinish(),
	)

	batchSize := 32
	for i := 0; i < len(items); i += batchSize {
		end := i + batchSize
		if end > len(items) {
			end = len(items)
		}
		batch := items[i:end]

		texts := make([]string, len(batch))
		for j, p := range batch {
			texts[j] = p.text
		}

		vecs, err := e.EmbedBatch(context.Background(), texts)
		if err != nil {
			bar.Add(len(batch))
			continue
		}

		for j, vec := range vecs {
			db.UpsertMessageEmbedding(batch[j].rowid, vec)
		}
		bar.Add(len(batch))
	}
	bar.Finish()
}

// cacheUser upserts a user into the cache.
func cacheUser(gaiaID, name, email string) {
	if db == nil || gaiaID == "" {
		return
	}
	db.UpsertUser(gaiaID, name, email)
}

// cacheConversation upserts a conversation into the cache.
func cacheConversation(id, name string, isDM bool, lastMsg string, lastTime int64) {
	if db == nil {
		return
	}
	db.UpsertConversation(id, name, isDM, lastMsg, lastTime)
}

// cacheMembership upserts a membership into the cache.
func cacheMembership(convID, userID string) {
	if db == nil || convID == "" || userID == "" {
		return
	}
	db.UpsertMembership(convID, userID)
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
		Method:        "cookie",
		Cookies:       cookies,
		XSRF:          session.XSRF(),
		ChromeProfile: profile,
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
		// Auto-refresh from Chrome if profile is known
		if creds.ChromeProfile != "" {
			session, err := refreshFromChrome(creds)
			if err == nil {
				return session, nil
			}
			fmt.Fprintf(os.Stderr, "Auto-refresh failed, using saved cookies: %v\n", err)
		}

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

// refreshFromChrome extracts fresh cookies from Chrome and bootstraps XSRF.
func refreshFromChrome(creds *config.Credentials) (auth.Session, error) {
	profiles, err := auth.FindChromeProfiles()
	if err != nil {
		return nil, err
	}

	var chromeDir string
	for _, p := range profiles {
		if p.Name == creds.ChromeProfile {
			chromeDir = p.Path
			break
		}
	}
	if chromeDir == "" {
		return nil, fmt.Errorf("Chrome profile %q not found", creds.ChromeProfile)
	}

	cookies, err := auth.ExtractChromeGoogleCookies(chromeDir)
	if err != nil {
		return nil, err
	}

	session, err := auth.NewCookieSession(cookies)
	if err != nil {
		return nil, err
	}

	if err := session.BootstrapXSRF(&http.Client{}); err != nil {
		return nil, err
	}

	creds.Cookies = cookies
	creds.XSRF = session.XSRF()
	config.SaveCredentials(creds)

	return session, nil
}

// conversationsCmd lists conversations.
func conversationsCmd() *cobra.Command {
	var limit int

	cmd := &cobra.Command{
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
				c := model.ConversationFromWorldItem(item)
				convos = append(convos, c)
				cacheConversation(model.FormatGroupID(item.GetGroupId()), c.Name, c.IsDM, c.LastMsg, c.LastTime.UnixMicro())
			}

			if limit > 0 && limit < len(convos) {
				convos = convos[:limit]
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
	cmd.Flags().IntVarP(&limit, "limit", "n", 0, "max number of conversations to show (0 = all)")
	return cmd
}

// messagesCmd fetches message history for a conversation.
func messagesCmd() *cobra.Command {
	var limit int
	var since string

	cmd := &cobra.Command{
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
			if since != "" {
				sinceTime, err := model.ParseSince(since)
				if err != nil {
					return err
				}
				fromTS = sinceTime.UnixMicro()
			}

			resp, err := chatAPI.CatchUpGroup(api.NewRequestHeader(), groupID, fromTS)
			if err != nil {
				return err
			}

			convID := args[0]
			var messages []model.Message
			for _, event := range resp.GetEvents() {
				for _, msg := range extractMessages(event) {
					m := model.MessageFromProto(msg)
					messages = append(messages, m)
					cacheMessage(convID, m.ID, m.SenderID, m.Text, m.Time.UnixMicro(), m.IsDeleted)
					cacheUser(m.SenderID, m.Sender, "")
				}
			}

			// Threaded Spaces: if catch_up_group returned no messages, try list_topics
			if len(messages) == 0 {
				topicsResp, err := chatAPI.ListTopics(api.NewRequestHeader(), groupID)
				if err == nil && len(topicsResp.GetTopics()) > 0 {
					for _, topic := range topicsResp.GetTopics() {
						for _, reply := range topic.GetReplies() {
							m := model.MessageFromProto(reply)
							messages = append(messages, m)
							cacheMessage(convID, m.ID, m.SenderID, m.Text, m.Time.UnixMicro(), m.IsDeleted)
							cacheUser(m.SenderID, m.Sender, "")
						}
					}
				}
			}

			if limit > 0 && limit < len(messages) {
				messages = messages[len(messages)-limit:]
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
	cmd.Flags().IntVarP(&limit, "limit", "n", 0, "max messages to show, most recent (0 = all)")
	cmd.Flags().StringVar(&since, "since", "", "only messages since duration ago (e.g. 24h, 168h)")
	return cmd
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
	var limit int
	var since string

	cmd := &cobra.Command{
		Use:   "recent",
		Short: "Show recent messages across all conversations",
		RunE: func(cmd *cobra.Command, args []string) error {
			session, err := loadSession()
			if err != nil {
				return err
			}

			client := transport.NewClient(session)
			chatAPI := api.New(client)

			dur := 24 * time.Hour
			if since != "" {
				d, err := time.ParseDuration(since)
				if err != nil {
					return fmt.Errorf("invalid --since duration: %w", err)
				}
				dur = d
			}

			fromTS := time.Now().Add(-dur).UnixMicro()
			resp, err := chatAPI.CatchUpUser(api.NewRequestHeader(), fromTS)
			if err != nil {
				return err
			}

			count := 0
			for _, event := range resp.GetEvents() {
				for _, msg := range extractMessages(event) {
					if limit > 0 && count >= limit {
						return nil
					}
					m := model.MessageFromProto(msg)
					gid := model.FormatGroupID(event.GetGroupId())
					sender := m.Sender
					if sender == "" {
						sender = m.SenderID
					}
					fmt.Printf("[%s] %s | %s: %s\n", m.Time.Format("15:04"), gid, sender, m.Text)
					count++
				}
			}

			return nil
		},
	}
	cmd.Flags().IntVarP(&limit, "limit", "n", 0, "max messages to show (0 = all)")
	cmd.Flags().StringVar(&since, "since", "24h", "how far back to look (e.g. 1h, 168h)")
	return cmd
}

// dmsCmd lists all DM contacts with resolved names.
func dmsCmd() *cobra.Command {
	var limit int

	cmd := &cobra.Command{
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
				if limit > 0 && len(contacts) >= limit {
					break
				}

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
	cmd.Flags().IntVarP(&limit, "limit", "n", 0, "max contacts to show (0 = all)")
	return cmd
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

// --- Search Command ---

// searchCmd performs semantic or keyword search across cached messages.
func searchCmd() *cobra.Command {
	var keyword bool
	var limit int
	var since string

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search cached messages",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if db == nil {
				return fmt.Errorf("cache not available")
			}

			sinceUsec := int64(0)
			if since != "" {
				t, err := model.ParseSince(since)
				if err != nil {
					return err
				}
				sinceUsec = t.UnixMicro()
			}

			if limit <= 0 {
				limit = 20
			}

			var results []cache.SearchResult
			var err error

			if keyword {
				results, err = db.KeywordSearch(args[0], limit, sinceUsec)
			} else {
				e := getEmbedder()
				if e == nil {
					return fmt.Errorf("embedder not available — cannot do semantic search. Use --keyword for text search.")
				}
				vec, embErr := e.Embed(context.Background(), args[0])
				if embErr != nil {
					return fmt.Errorf("embedding failed: %w", embErr)
				}
				results, err = db.SemanticSearch(vec, limit, sinceUsec)
			}

			if err != nil {
				return err
			}

			if len(results) == 0 {
				fmt.Println("No results. Run 'gchat --load-data-since 168h' to populate the cache first.")
				return nil
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(results)
			}

			for _, r := range results {
				ts := time.UnixMicro(r.CreatedAt).Format("2006-01-02 15:04")
				sender := r.SenderName
				if sender == "" {
					sender = r.SenderID
				}
				fmt.Printf("[%s] %s | %s: %s\n", ts, r.ConversationID, sender, r.Text)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&keyword, "keyword", false, "use keyword (FTS5) search instead of semantic")
	cmd.Flags().IntVarP(&limit, "limit", "n", 20, "max results")
	cmd.Flags().StringVar(&since, "since", "", "only search messages since (e.g. 168h, 2024-01-15)")
	return cmd
}

// --- Cache Management ---

// cacheCmd provides cache inspection and management.
func cacheCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Manage local cache",
	}
	cmd.AddCommand(cacheStatsCmd())
	cmd.AddCommand(cacheClearCmd())
	return cmd
}

// cacheStatsCmd shows cache statistics.
func cacheStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Show cache statistics",
		RunE: func(cmd *cobra.Command, args []string) error {
			if db == nil {
				return fmt.Errorf("cache not available")
			}

			stats, err := db.GetStats()
			if err != nil {
				return err
			}

			dbPath, _ := config.CachePath()
			info, _ := os.Stat(dbPath)
			dbSize := "unknown"
			if info != nil {
				dbSize = fmt.Sprintf("%.1f MB", float64(info.Size())/1024/1024)
			}

			fmt.Printf("Cache: %s (%s)\n", dbPath, dbSize)
			fmt.Printf("  Conversations: %d\n", stats.Conversations)
			fmt.Printf("  Messages:      %d\n", stats.Messages)
			fmt.Printf("  Users:         %d\n", stats.Users)
			fmt.Printf("  Memberships:   %d\n", stats.Memberships)
			fmt.Printf("  Embeddings:    %d\n", stats.Embeddings)
			fmt.Printf("  Model:         %v\n", embed.ModelExists())
			return nil
		},
	}
}

// cacheClearCmd wipes the cache.
func cacheClearCmd() *cobra.Command {
	var clearModels bool

	cmd := &cobra.Command{
		Use:   "clear",
		Short: "Clear cached data",
		RunE: func(cmd *cobra.Command, args []string) error {
			if db != nil {
				if err := db.ClearAll(); err != nil {
					return err
				}
				fmt.Println("Cache cleared.")
			}

			if clearModels {
				modelsDir, err := config.ModelsDir()
				if err == nil {
					os.RemoveAll(modelsDir)
					fmt.Println("Models deleted.")
				}
			}

			return nil
		},
	}
	cmd.Flags().BoolVar(&clearModels, "models", false, "also delete downloaded models")
	return cmd
}

// --- Load Data ---

// loadCmd creates the `gchat load` command.
func loadCmd() *cobra.Command {
	var sync bool

	cmd := &cobra.Command{
		Use:   "load [since]",
		Short: "Load and cache data since a time (e.g. 168h, 720h, 2024-01-15) or --sync for incremental",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if sync {
				return runLoadSync()
			}
			if len(args) == 0 {
				return fmt.Errorf("provide a time (e.g. '168h', '2024-01-15') or use --sync")
			}
			return runLoadDataSince(args[0])
		},
	}
	cmd.Flags().BoolVar(&sync, "sync", false, "load new data since last import")
	return cmd
}

// runLoadSync loads data since the last successful sync.
// Uses the stored last_load_time from cache_meta, which is set at the START
// of each load (not the end) to avoid gaps from messages arriving during load.
func runLoadSync() error {
	if db == nil {
		return fmt.Errorf("cache not available")
	}

	lastLoad, err := db.GetMeta("last_load_time")
	if err != nil {
		return err
	}
	if lastLoad == "" {
		return fmt.Errorf("no previous load found — run 'gchat load 720h' first to do an initial import")
	}

	fmt.Fprintf(os.Stderr, "Syncing since last load (%s)...\n", lastLoad)
	return runLoadDataSince(lastLoad)
}

// runLoadDataSince fetches all conversations, messages, and members since the given time.
// Fetches all conversations, messages, and members since the given time.
func runLoadDataSince(sinceStr string) error {
	sinceTime, err := model.ParseSince(sinceStr)
	if err != nil {
		return err
	}
	fromTS := sinceTime.UnixMicro()

	// Record load start time NOW so the next --sync covers the gap
	if db != nil {
		db.SetMeta("last_load_time", time.Now().UTC().Format(time.RFC3339))
	}

	session, err := loadSession()
	if err != nil {
		return err
	}

	client := transport.NewClient(session)
	chatAPI := api.New(client)

	// Step 1: Fetch conversations
	fmt.Fprintf(os.Stderr, "[1/4] Fetching conversations...\n")
	resp, err := chatAPI.PaginatedWorld(api.NewRequestHeader())
	if err != nil {
		return fmt.Errorf("load: conversations: %w", err)
	}

	convos := resp.GetWorldItems()
	for _, item := range convos {
		c := model.ConversationFromWorldItem(item)
		cacheConversation(model.FormatGroupID(item.GetGroupId()), c.Name, c.IsDM, c.LastMsg, c.LastTime.UnixMicro())
	}
	fmt.Fprintf(os.Stderr, "  %d conversations cached\n", len(convos))

	// Step 2: Fetch messages for each conversation
	fmt.Fprintf(os.Stderr, "[2/4] Fetching messages...\n")
	bar := progressbar.NewOptions(len(convos),
		progressbar.OptionSetDescription("  Messages"),
		progressbar.OptionSetWriter(os.Stderr),
		progressbar.OptionShowCount(),
		progressbar.OptionClearOnFinish(),
	)

	totalMsgs := 0
	for _, item := range convos {
		gid := item.GetGroupId()
		convID := model.FormatGroupID(gid)

		// Try catch_up_group first (works for DMs and flat Spaces)
		msgCount := 0
		catchResp, err := chatAPI.CatchUpGroup(api.NewRequestHeader(), gid, fromTS)
		if err == nil {
			for _, event := range catchResp.GetEvents() {
				for _, msg := range extractMessages(event) {
					m := model.MessageFromProto(msg)
					cacheMessage(convID, m.ID, m.SenderID, m.Text, m.Time.UnixMicro(), m.IsDeleted)
					cacheUser(m.SenderID, m.Sender, "")
					msgCount++
				}
			}
		}

		// Threaded Spaces: if no messages from catch_up, try list_topics
		if msgCount == 0 {
			topicsResp, topErr := chatAPI.ListTopics(api.NewRequestHeader(), gid)
			if topErr == nil {
				for _, topic := range topicsResp.GetTopics() {
					for _, reply := range topic.GetReplies() {
						m := model.MessageFromProto(reply)
						cacheMessage(convID, m.ID, m.SenderID, m.Text, m.Time.UnixMicro(), m.IsDeleted)
						cacheUser(m.SenderID, m.Sender, "")
						msgCount++
					}
				}
			}
		}

		totalMsgs += msgCount
		bar.Add(1)
	}
	bar.Finish()
	fmt.Fprintf(os.Stderr, "  %d messages cached\n", totalMsgs)

	// Step 3: Fetch members
	fmt.Fprintf(os.Stderr, "[3/4] Fetching members...\n")
	bar = progressbar.NewOptions(len(convos),
		progressbar.OptionSetDescription("  Members"),
		progressbar.OptionSetWriter(os.Stderr),
		progressbar.OptionShowCount(),
		progressbar.OptionClearOnFinish(),
	)

	selfResp, _ := chatAPI.GetSelfUserStatus(api.NewRequestHeader())
	selfID := ""
	if selfResp != nil {
		selfID = selfResp.GetUserStatus().GetUserId().GetId()
		if db != nil {
			db.SetMeta("self_gaia_id", selfID)
		}
	}

	totalUsers := 0
	for _, item := range convos {
		gid := item.GetGroupId()
		convID := model.FormatGroupID(gid)

		members, err := chatAPI.ListMembers(api.NewRequestHeader(), gid)
		if err != nil {
			bar.Add(1)
			continue
		}

		for _, ms := range members.GetMemberships() {
			mid := ms.GetId().GetMemberId()
			uid := mid.GetUserId().GetId()
			if uid == "" {
				continue
			}
			cacheMembership(convID, uid)

			memberResp, err := chatAPI.GetMembers(api.NewRequestHeader(), []*pb.MemberId{mid})
			if err == nil {
				for _, m := range memberResp.GetMembers() {
					if u := m.GetUser(); u != nil {
						cacheUser(u.GetUserId().GetId(), u.GetName(), u.GetEmail())
						totalUsers++
					}
				}
			}
		}
		bar.Add(1)
	}
	bar.Finish()
	fmt.Fprintf(os.Stderr, "  %d users cached\n", totalUsers)

	// Step 4: Embed new messages
	fmt.Fprintf(os.Stderr, "[4/5] Embedding new messages...\n")
	embedCachedMessages()

	// Step 5: Print summary
	if db != nil {
		stats, _ := db.GetStats()
		if stats != nil {
			fmt.Fprintf(os.Stderr, "[5/5] Done! Cache: %d conversations, %d messages, %d users, %d embeddings\n",
				stats.Conversations, stats.Messages, stats.Users, stats.Embeddings)
		}
	}

	return nil
}

// --- Mentions Command ---

// mentionsCmd shows messages where the authenticated user was @mentioned.
func mentionsCmd() *cobra.Command {
	var limit int
	var since string

	cmd := &cobra.Command{
		Use:   "mentions",
		Short: "Show messages where you were @mentioned",
		RunE: func(cmd *cobra.Command, args []string) error {
			if db == nil {
				return fmt.Errorf("cache not available — run 'gchat load 720h' first")
			}

			selfID := ""
			if val, _ := db.GetMeta("self_gaia_id"); val != "" {
				selfID = val
			} else {
				session, err := loadSession()
				if err != nil {
					return err
				}
				client := transport.NewClient(session)
				chatAPI := api.New(client)
				resp, _ := chatAPI.GetSelfUserStatus(api.NewRequestHeader())
				if resp != nil {
					selfID = resp.GetUserStatus().GetUserId().GetId()
					db.SetMeta("self_gaia_id", selfID)
				}
			}
			if selfID == "" {
				return fmt.Errorf("cannot determine your user ID")
			}

			sinceUsec := int64(0)
			if since != "" {
				t, err := model.ParseSince(since)
				if err != nil {
					return err
				}
				sinceUsec = t.UnixMicro()
			}

			if limit <= 0 {
				limit = 50
			}

			results, err := db.SearchMentions(selfID, limit, sinceUsec)
			if err != nil {
				return err
			}

			if len(results) == 0 {
				fmt.Println("No @mentions found in cache. Run 'gchat load 720h' to populate.")
				return nil
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(results)
			}

			for _, r := range results {
				ts := time.UnixMicro(r.CreatedAt).Format("2006-01-02 15:04")
				sender := r.SenderName
				if sender == "" {
					sender = r.SenderID
				}
				fmt.Printf("[%s] %s | %s: %s\n", ts, r.ConversationID, sender, r.Text)
			}
			return nil
		},
	}
	cmd.Flags().IntVarP(&limit, "limit", "n", 50, "max results")
	cmd.Flags().StringVar(&since, "since", "", "only mentions since (e.g. 168h, 2024-01-15)")
	return cmd
}

// openBrowser tries to open a URL in the default browser.
func openBrowser(url string) {
	exec.Command("open", url).Start()
}
