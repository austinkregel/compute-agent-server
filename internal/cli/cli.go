package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Action represents the result of executing a CLI command.
type Action int

const (
	ActionContinue Action = iota
	ActionQuit
)

// ClientInfo is the minimal client info the CLI needs.
type ClientInfo struct {
	ClientID string
	Hostname string
	Platform string
}

// Store is the interface the CLI uses to interact with server state.
type Store interface {
	ListClients() []ClientInfo
	GetClientStats(clientID string) map[string]any
	HasClient(clientID string) bool
	SendCommand(clientID, event string, payload any) bool
}

// CLI provides an interactive command-line interface for the server.
type CLI struct {
	store Store
	in    io.Reader
	out   io.Writer
}

// NewCLI creates a new CLI instance.
func NewCLI(store Store, in io.Reader, out io.Writer) *CLI {
	return &CLI{
		store: store,
		in:    in,
		out:   out,
	}
}

// Run starts the interactive command loop. It blocks until "quit" is entered
// or the context is cancelled.
func (c *CLI) Run(ctx context.Context) {
	scanner := bufio.NewScanner(c.in)
	fmt.Fprintln(c.out, "Server CLI ready. Type \"help\" for commands.")

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		fmt.Fprint(c.out, "server> ")
		if !scanner.Scan() {
			return // EOF or error
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		cmd, args := parseCommand(line)
		action := c.Execute(cmd, args)
		if action == ActionQuit {
			return
		}
	}
}

// Execute runs a single CLI command and returns the resulting action.
func (c *CLI) Execute(cmd string, args []string) Action {
	switch cmd {
	case "help":
		c.printHelp()
	case "list":
		c.listClients()
	case "stats":
		c.showStats(args)
	case "admin":
		c.adminCommand(args)
	case "send":
		c.sendRaw(args)
	case "quit", "exit":
		fmt.Fprintln(c.out, "Shutting down...")
		return ActionQuit
	default:
		fmt.Fprintf(c.out, "Unknown command %q; type \"help\" for a list.\n", cmd)
	}
	return ActionContinue
}

func (c *CLI) printHelp() {
	fmt.Fprintln(c.out, "Available commands:")
	fmt.Fprintln(c.out, "  list                              List connected clients")
	fmt.Fprintln(c.out, "  stats <clientId>                  Show last known stats for a client")
	fmt.Fprintln(c.out, "  admin <clientId> <command...>      Send an admin command to a client")
	fmt.Fprintln(c.out, "  send <clientId> <event> <json>     Send a raw signed command")
	fmt.Fprintln(c.out, "  quit                              Exit server")
}

func (c *CLI) listClients() {
	clients := c.store.ListClients()
	if len(clients) == 0 {
		fmt.Fprintln(c.out, "No clients connected.")
		return
	}
	for _, cl := range clients {
		extra := ""
		if cl.Hostname != "" {
			extra += " (" + cl.Hostname
			if cl.Platform != "" {
				extra += ", " + cl.Platform
			}
			extra += ")"
		}
		fmt.Fprintf(c.out, "  - %s%s\n", cl.ClientID, extra)
	}
}

func (c *CLI) showStats(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(c.out, "Usage: stats <clientId>")
		return
	}
	clientID := args[0]
	stats := c.store.GetClientStats(clientID)
	if stats == nil {
		fmt.Fprintln(c.out, "No stats for client.")
		return
	}
	data, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		fmt.Fprintf(c.out, "Error formatting stats: %v\n", err)
		return
	}
	fmt.Fprintln(c.out, string(data))
}

func (c *CLI) adminCommand(args []string) {
	if len(args) < 2 {
		fmt.Fprintln(c.out, "Usage: admin <clientId> <command...>")
		return
	}
	clientID := args[0]
	command := strings.Join(args[1:], " ")

	if !c.store.HasClient(clientID) {
		fmt.Fprintf(c.out, "Client %q is offline.\n", clientID)
		return
	}

	ok := c.store.SendCommand(clientID, "admin_run", map[string]any{
		"cmd": map[string]any{"command": command},
	})
	if ok {
		fmt.Fprintln(c.out, "Admin command sent (signed).")
	} else {
		fmt.Fprintln(c.out, "Failed to send command.")
	}
}

func (c *CLI) sendRaw(args []string) {
	if len(args) < 3 {
		fmt.Fprintln(c.out, "Usage: send <clientId> <eventName> <payloadJson>")
		return
	}
	clientID := args[0]
	event := args[1]
	payloadStr := strings.Join(args[2:], " ")

	var payload any
	if err := json.Unmarshal([]byte(payloadStr), &payload); err != nil {
		fmt.Fprintln(c.out, "Invalid JSON payload.")
		return
	}

	if !c.store.HasClient(clientID) {
		fmt.Fprintf(c.out, "Client %q is offline.\n", clientID)
		return
	}

	ok := c.store.SendCommand(clientID, event, payload)
	if ok {
		fmt.Fprintln(c.out, "Sent (signed).")
	} else {
		fmt.Fprintln(c.out, "Failed to send.")
	}
}

// parseCommand splits a line into command and arguments.
func parseCommand(line string) (string, []string) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "", nil
	}
	return fields[0], fields[1:]
}
