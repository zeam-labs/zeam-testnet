package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

var cognition *Cognition
var logChannel = make(chan tea.Msg, 1000)
var uiActive = false

type LogEvent struct{ Msg string }
type InputEvent struct{ Content string }

type dashboardModel struct {
	presenceID string
	l2Key      string
	l3Key      string
	input      string
	cursor     int
	output     string
	l2log      []string
	logDump    []string
	cpu        string
	storage    string
	nodeCount  int
	ggufFlow   []string
}

func StartPresenceConsoleUI(presenceID string) {
	l2Key := "presence:L2:" + presenceID
	l3Key := "presence:L3:" + presenceID

	if Chains[l2Key] == nil {
		Chains[l2Key] = NewChain(l2Key)
	}
	if Chains[l3Key] == nil {
		Chains[l3Key] = NewChain(l3Key)
	}

	uiActive = true

	p := tea.NewProgram(dashboardModel{
		presenceID: presenceID,
		l2Key:      l2Key,
		l3Key:      l3Key,
	})

	if err := p.Start(); err != nil {
		Log("💥 UI error: %v", err)
	}
}

func (m dashboardModel) Init() tea.Cmd {

	CaptureMeshHealth(cognition)

	return func() tea.Msg {
		return <-logChannel
	}
}

func (m dashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {

	case tea.KeyMsg:
		switch v.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			uiActive = false
			return m, tea.Quit

		case tea.KeyEnter:
			text := strings.TrimSpace(m.input)
			if text == "" {
				m.input = ""
				m.cursor = 0
				return m, waitForLog()
			}

			input := Input{
				Content:   text,
				Source:    m.presenceID,
				Timestamp: time.Now().UTC(),
				ChainKey:  m.l2Key,
				Type:      "",
			}

			Chains[m.l2Key].Mint(context.Background(), input)
			RunAnalysisPass(cognition, m.presenceID, Chains[m.l2Key], Chains[m.l3Key])

			m.l2log = append(m.l2log, text)
			if len(m.l2log) > 5 {
				m.l2log = m.l2log[len(m.l2log)-5:]
			}

			if entries := Chains[m.l3Key].Entries; len(entries) > 0 {
				m.output = entries[len(entries)-1].Content
			} else {
				m.output = ""
			}

			m.input = ""
			m.cursor = 0
			return m, waitForLog()

		case tea.KeyBackspace:
			if m.cursor > 0 {
				m.input = m.input[:m.cursor-1] + m.input[m.cursor:]
				m.cursor--
			}
			return m, waitForLog()

		case tea.KeyLeft:
			if m.cursor > 0 {
				m.cursor--
			}
			return m, waitForLog()

		case tea.KeyRight:
			if m.cursor < len(m.input) {
				m.cursor++
			}
			return m, waitForLog()

		default:
			m.input = m.input[:m.cursor] + v.String() + m.input[m.cursor:]
			m.cursor++
			return m, waitForLog()
		}

	case LogEvent:
		switch {
		case strings.HasPrefix(v.Msg, "📦"):
			m.ggufFlow = append(m.ggufFlow, v.Msg)
			if len(m.ggufFlow) > 10 {
				m.ggufFlow = m.ggufFlow[len(m.ggufFlow)-10:]
			}
		case strings.HasPrefix(v.Msg, "⚙️"):
			// Optionally route to system log
		case strings.HasPrefix(v.Msg, "🧠 CPU"):
			// Already tracked by MeshHealthSnapshot
		default:
			m.logDump = append(m.logDump, v.Msg)
			if len(m.logDump) > 5 {
				m.logDump = m.logDump[len(m.logDump)-5:]
			}
		}
		return m, waitForLog()

	case MeshHealthSnapshot:
		m.cpu = v.CPU
		m.storage = v.Storage
		m.nodeCount = v.NodeCount

		seen := map[string]bool{}
		var latest []string
		for i := len(v.Flow) - 1; i >= 0; i-- {
			f := v.Flow[i]
			if !seen[f] && len(latest) < 10 {
				latest = append([]string{f}, latest...)
				seen[f] = true
			}
		}
		m.ggufFlow = latest

		return m, waitForLog()

	default:
		return m, waitForLog()
	}
}

func Log(format string, args ...any) string {
	msg := fmt.Sprintf(format, args...)

	// Write to stderr if no UI
	if !uiActive {
		fmt.Fprintln(os.Stderr, msg)
		os.Stderr.Sync() // flush to prevent SSH hang
	}

	// Always queue logs to channel when UI is active
	if uiActive {
		select {
		case logChannel <- LogEvent{Msg: msg}:
		default:
			// drop if overfilled to protect the pipe
		}
	}

	return msg
}

func (m dashboardModel) View() string {
	var b strings.Builder

	// HEADER
	b.WriteString("ZEAM | BlockMesh::OS\n")
	b.WriteString(strings.Repeat("─", 50) + "\n")
	b.WriteString(fmt.Sprintf("⚙️ CPU: %s   💾 %s   🌐 Nodes: %d\n", m.cpu, m.storage, m.nodeCount))
	b.WriteString(strings.Repeat("─", 50) + "\n")

	// STORAGE FLOW
	if len(m.ggufFlow) > 0 {
		b.WriteString("📦 Shard Routing:\n")
		seen := map[string]bool{}
		count := 0
		for i := len(m.ggufFlow) - 1; i >= 0 && count < 10; i-- {
			entry := m.ggufFlow[i]
			if !seen[entry] {
				b.WriteString("   " + entry + "\n")
				seen[entry] = true
				count++
			}
		}
		b.WriteString(strings.Repeat("─", 50) + "\n")
	}

	// MEMORY LOG (L2)
	b.WriteString("📜 Memory Log (L2):\n")
	if len(m.l2log) == 0 {
		b.WriteString("   (no L2 memory yet)\n")
	} else {
		for _, line := range m.l2log {
			b.WriteString("   " + line + "\n")
		}
	}
	b.WriteString(strings.Repeat("─", 50) + "\n")

	// OUTPUT
	b.WriteString("🪞 Output:\n")
	if strings.TrimSpace(m.output) != "" {
		b.WriteString("   " + m.output + "\n")
	} else {
		b.WriteString("   (no output)\n")
	}
	b.WriteString(strings.Repeat("─", 50) + "\n")

	// INPUT with safe cursor insert
	cursor := m.cursor
	if cursor > len(m.input) {
		cursor = len(m.input)
	}
	inputWithCursor := m.input[:cursor] + "_" + m.input[cursor:]
	b.WriteString("🧠 Input:\n> " + inputWithCursor + "\n")
	b.WriteString(strings.Repeat("─", 50) + "\n")

	// LOG DUMP
	b.WriteString("🧾 Log Dump:\n")
	if len(m.logDump) == 0 {
		b.WriteString("   (no recent logs)\n")
	} else {
		for _, line := range m.logDump {
			b.WriteString("   " + line + "\n")
		}
	}
	b.WriteString(strings.Repeat("─", 50) + "\n")

	return b.String()
}

func waitForLog() tea.Cmd {
	return func() tea.Msg {
		select {
		case msg := <-logChannel:
			return msg
		case <-time.After(100 * time.Millisecond):
			return nil
		}
	}
}
