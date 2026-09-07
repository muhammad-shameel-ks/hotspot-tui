// Hotspot TUI: 5 GHz AP on wlp1s0 via create_ap daemon mode.
// Esc detaches (TUI quits, AP keeps running). Stop kills the AP.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mdp/qrterminal/v3"
	"rsc.io/qr"
)

const (
	wifiIface = "wlp1s0"
	cfgFile   = "/etc/create_ap.conf"
	pidFile   = "/tmp/hotspot-tui.pid"
	logFile   = "/tmp/hotspot-tui.log"
)

var (
	titleStyle = lipgloss.NewStyle().Bold(true)
	okStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true)
	badStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	dimStyle   = lipgloss.NewStyle().Faint(true)
	selStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Bold(true)
)

type statusMsg struct {
	running        bool
	info, log, err string
	ssid, pass     string
}
type doneMsg struct{ text, err string }

type model struct {
	cursor  int
	choices []string
	status  string
	info    string
	log     string
	msg     string
	busy    bool
	ssid    string
	pass    string
	showQR  bool
	editing bool
	editFoc int // 0 ssid, 1 pass
	ssidIn  textinput.Model
	passIn  textinput.Model
}

func newModel() model {
	si := textinput.New()
	si.Placeholder = "SSID"
	si.CharLimit = 32
	pi := textinput.New()
	pi.Placeholder = "password (min 8 chars)"
	pi.EchoMode = textinput.EchoPassword
	ssid, pass := readCreds()
	si.SetValue(ssid)
	pi.SetValue(pass)
	return model{
		choices: []string{"Start hotspot", "Stop hotspot", "Edit SSID/password", "Show/hide QR", "Refresh status"},
		status:  "...",
		ssid:    ssid,
		pass:    pass,
		ssidIn:  si,
		passIn:  pi,
	}
}

func (m model) Init() tea.Cmd { return refreshCmd }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case statusMsg:
		if msg.running {
			m.status = "RUNNING"
		} else {
			m.status = "STOPPED"
		}
		m.info, m.log = msg.info, msg.log
		if msg.ssid != "" {
			m.ssid = msg.ssid
		}
		if msg.pass != "" {
			m.pass = msg.pass
		}
		if msg.err != "" {
			m.msg = "status: " + msg.err
		}
		m.busy = false
		return m, nil
	case doneMsg:
		m.msg = msg.text
		if msg.err != "" {
			m.msg += ": " + msg.err
		}
		m.busy = false
		return m, refreshCmd
	case tea.KeyMsg:
		if m.editing {
			switch msg.String() {
			case "esc":
				m.editing = false
				m.ssidIn.Blur()
				m.passIn.Blur()
				m.msg = "edit cancelled"
				return m, nil
			case "tab", "shift+tab":
				if m.editFoc == 0 {
					m.editFoc = 1
					m.ssidIn.Blur()
					m.passIn.Focus()
				} else {
					m.editFoc = 0
					m.passIn.Blur()
					m.ssidIn.Focus()
				}
				return m, nil
			case "enter":
				ssid := strings.TrimSpace(m.ssidIn.Value())
				pass := m.passIn.Value()
				if err := saveCreds(ssid, pass); err != nil {
					m.msg = "save: " + err.Error()
					return m, nil
				}
				m.editing = false
				m.ssidIn.Blur()
				m.passIn.Blur()
				m.msg = "saved - press s to restart hotspot"
				m.busy = true
				return m, refreshCmd
			}
			var cmd tea.Cmd
			if m.editFoc == 0 {
				m.ssidIn, cmd = m.ssidIn.Update(msg)
			} else {
				m.passIn, cmd = m.passIn.Update(msg)
			}
			return m, cmd
		}
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			return m, tea.Quit // detach: daemon AP keeps running
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.choices)-1 {
				m.cursor++
			}
		case "s":
			if !m.busy {
				m.busy, m.msg = true, "starting..."
				return m, startCmd
			}
		case "x":
			if !m.busy {
				m.busy, m.msg = true, "stopping..."
				return m, stopCmd
			}
		case "e":
			if !m.busy {
				m.editing = true
				m.editFoc = 0
				m.ssidIn.SetValue(m.ssid)
				m.passIn.SetValue(m.pass)
				m.ssidIn.Focus()
				m.msg = "editing - tab switches field, enter saves, esc cancels"
			}
		case "v":
			m.showQR = !m.showQR
		case "r":
			if !m.busy {
				m.busy = true
				return m, refreshCmd
			}
		case "enter":
			if m.busy {
				return m, nil
			}
			m.busy = true
			switch m.cursor {
			case 0:
				m.msg = "starting..."
				return m, startCmd
			case 1:
				m.msg = "stopping..."
				return m, stopCmd
			case 2:
				m.busy = false
				m.editing = true
				m.editFoc = 0
				m.ssidIn.SetValue(m.ssid)
				m.passIn.SetValue(m.pass)
				m.ssidIn.Focus()
				m.msg = "editing - tab switches field, enter saves, esc cancels"
				return m, nil
			case 3:
				m.busy = false
				m.showQR = !m.showQR
				return m, nil
			default:
				m.msg = "refreshing..."
				return m, refreshCmd
			}
		}
	}
	return m, nil
}

func (m model) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("hotspot 5 GHz (ch 149)") + "\n")
	st := badStyle.Render(m.status)
	if m.status == "RUNNING" {
		st = okStyle.Render(m.status)
	}
	if m.busy {
		st += dimStyle.Render(" (working...)")
	}
	b.WriteString("status: " + st + "\n")
	b.WriteString(fmt.Sprintf("ssid: %s   pass: %s\n", m.ssid, maskPass(m.pass)))
	if m.info != "" {
		b.WriteString(dimStyle.Render(m.info) + "\n")
	}
	b.WriteString("\n")
	if m.editing {
		b.WriteString("SSID: " + m.ssidIn.View() + "\n")
		b.WriteString("Pass: " + m.passIn.View() + "\n")
		b.WriteString(dimStyle.Render("tab switch  enter save  esc cancel") + "\n")
	} else {
		for i, c := range m.choices {
			if i == m.cursor {
				b.WriteString(selStyle.Render("> "+c) + "\n")
			} else {
				b.WriteString("  " + c + "\n")
			}
		}
	}
	if m.showQR && !m.editing {
		b.WriteString("\n" + dimStyle.Render("scan to join:") + "\n")
		b.WriteString(qrString(m.ssid, m.pass) + "\n")
	}
	if m.msg != "" {
		b.WriteString("\n" + m.msg + "\n")
	}
	if m.log != "" {
		b.WriteString("\n" + dimStyle.Render("--- log tail ---") + "\n" + m.log + "\n")
	}
	b.WriteString(dimStyle.Render("\n↑/↓ move  enter select  s start  x stop  e edit  v QR  r refresh  esc detach (AP keeps running)") + "\n")
	return b.String()
}

func maskPass(p string) string {
	if p == "" {
		return "(empty)"
	}
	return strings.Repeat("*", len(p)) + fmt.Sprintf(" (%d chars)", len(p))
}

// wifiPayload builds the standard WIFI: QR string phones understand.
func wifiPayload(ssid, pass string) string {
	esc := func(s string) string {
		r := strings.ReplaceAll(s, `\`, `\\`)
		for _, c := range []string{";", ",", ":", `"`} {
			r = strings.ReplaceAll(r, c, `\`+c)
		}
		return r
	}
	return fmt.Sprintf("WIFI:T:WPA;S:%s;P:%s;H:false;;", esc(ssid), esc(pass))
}

func qrString(ssid, pass string) string {
	if ssid == "" || pass == "" {
		return "(set SSID and password first)"
	}
	var buf bytes.Buffer
	qrterminal.GenerateHalfBlock(wifiPayload(ssid, pass), qr.M, &buf)
	return buf.String()
}

func refreshCmd() tea.Msg {
	running := apRunning()
	info := apInfo()
	log := tailFile(logFile, 8)
	ssid, pass := readCreds()
	return statusMsg{running: running, info: info, log: log, ssid: ssid, pass: pass}
}

func startCmd() tea.Msg {
	// Fresh start: clear stale AP, drop station so the single-channel
	// iwlwifi combo does not force a 2.4 GHz follow.
	exec.Command("sudo", "create_ap", "--stop", wifiIface).Run()
	exec.Command("nmcli", "device", "disconnect", wifiIface).Run()
	exec.Command("nmcli", "device", "set", wifiIface, "autoconnect", "no").Run()
	out, err := exec.Command("sudo", "create_ap", "--config", cfgFile,
		"--daemon", "--pidfile", pidFile, "--logfile", logFile).CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		return doneMsg{text: "start failed", err: text}
	}
	if text == "" {
		text = "AP start requested"
	}
	return doneMsg{text: text}
}

func stopCmd() tea.Msg {
	out, err := exec.Command("sudo", "create_ap", "--stop", wifiIface).CombinedOutput()
	exec.Command("nmcli", "device", "set", wifiIface, "autoconnect", "yes").Run()
	exec.Command("nmcli", "device", "connect", wifiIface).Run()
	text := strings.TrimSpace(string(out))
	if text == "" {
		text = "AP stopped"
	}
	if err != nil {
		return doneMsg{text: "stop", err: text}
	}
	return doneMsg{text: text}
}

// readCreds parses SSID/PASSPHRASE out of the root-owned config.
func readCreds() (ssid, pass string) {
	ssid = "zeus"
	out, err := exec.Command("sudo", "cat", cfgFile).CombinedOutput()
	if err != nil {
		// No sudo yet (e.g. --status before auth): fall back to plain read.
		data, rerr := os.ReadFile(cfgFile)
		if rerr != nil {
			return ssid, ""
		}
		out = data
	}
	for _, line := range strings.Split(string(out), "\n") {
		if v, ok := strings.CutPrefix(line, "SSID="); ok {
			ssid = v
		}
		if v, ok := strings.CutPrefix(line, "PASSPHRASE="); ok {
			pass = v
		}
	}
	return ssid, pass
}

// saveCreds rewrites SSID/PASSPHRASE in place via sudo tee (no shell).
func saveCreds(ssid, pass string) error {
	if len(ssid) == 0 || len(ssid) > 32 {
		return fmt.Errorf("SSID must be 1-32 chars")
	}
	if len(pass) < 8 {
		return fmt.Errorf("password must be at least 8 chars for WPA2")
	}
	if strings.ContainsAny(ssid+pass, "\n") {
		return fmt.Errorf("SSID/password must not contain newlines")
	}
	out, err := exec.Command("sudo", "cat", cfgFile).CombinedOutput()
	if err != nil {
		return fmt.Errorf("cannot read config: %s", strings.TrimSpace(string(out)))
	}
	lines := strings.Split(string(out), "\n")
	seenS, seenP := false, false
	for i, line := range lines {
		if strings.HasPrefix(line, "SSID=") {
			lines[i] = "SSID=" + ssid
			seenS = true
		}
		if strings.HasPrefix(line, "PASSPHRASE=") {
			lines[i] = "PASSPHRASE=" + pass
			seenP = true
		}
	}
	if !seenS {
		lines = append(lines, "SSID="+ssid)
	}
	if !seenP {
		lines = append(lines, "PASSPHRASE="+pass)
	}
	cmd := exec.Command("sudo", "tee", cfgFile)
	cmd.Stdin = strings.NewReader(strings.Join(lines, "\n"))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("cannot write config: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func apRunning() bool {
	if out, err := exec.Command("create_ap", "--list-running").CombinedOutput(); err == nil {
		if strings.Contains(string(out), wifiIface) || strings.Contains(string(out), "ap0") {
			return true
		}
	}
	return ifaceExists("ap0")
}

func apInfo() string {
	out, err := exec.Command("iw", "dev", "ap0", "info").CombinedOutput()
	if err != nil {
		if apRunning() {
			return "ap0 up (details need sudo for full iw output)"
		}
		return "AP down. Start it to broadcast on 149."
	}
	var ssid, ch string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ssid ") {
			ssid = strings.TrimPrefix(line, "ssid ")
		}
		if strings.HasPrefix(line, "channel ") {
			ch = line
		}
	}
	if ssid == "" {
		ssid = "?"
	}
	if ch == "" {
		ch = "channel ?"
	}
	return fmt.Sprintf("ssid %s, %s", ssid, ch)
}

func ifaceExists(name string) bool {
	return exec.Command("ip", "link", "show", name).Run() == nil
}

func tailFile(path string, n int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func main() {
	statusFlag := flag.Bool("status", false, "print AP status and exit")
	startFlag := flag.Bool("start", false, "start AP (daemon) and exit")
	stopFlag := flag.Bool("stop", false, "stop AP and exit")
	qrFlag := flag.Bool("qr", false, "print WiFi QR code and exit")
	flag.Parse()

	switch {
	case *statusFlag:
		if apRunning() {
			fmt.Println("RUNNING " + apInfo())
		} else {
			fmt.Println("STOPPED")
		}
		return
	case *qrFlag:
		ssid, pass := readCreds()
		fmt.Println(wifiPayload(ssid, pass))
		fmt.Println(qrString(ssid, pass))
		return
	case *startFlag:
		m := startCmd().(doneMsg)
		fmt.Println(m.text)
		if m.err != "" {
			fmt.Fprintln(os.Stderr, m.err)
			os.Exit(1)
		}
		return
	case *stopFlag:
		m := stopCmd().(doneMsg)
		fmt.Println(m.text)
		if m.err != "" {
			fmt.Fprintln(os.Stderr, m.err)
			os.Exit(1)
		}
		return
	}

	// Pre-auth sudo so password prompt does not corrupt the TUI.
	exec.Command("sudo", "-v").Run()

	if _, err := tea.NewProgram(newModel()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "tui:", err)
		os.Exit(1)
	}
}
