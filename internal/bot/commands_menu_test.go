package bot

import (
	"reflect"
	"regexp"
	"slices"
	"testing"

	tele "gopkg.in/telebot.v3"
)

func commandTexts(cmds []tele.Command) []string {
	out := make([]string, len(cmds))
	for i, c := range cmds {
		out[i] = c.Text
	}
	return out
}

func TestGroupMenu(t *testing.T) {
	want := []string{"play", "stats", "rating", "setnick", "learn", "help"}
	if got := commandTexts(groupCommands()); !reflect.DeepEqual(got, want) {
		t.Errorf("group menu = %v, want %v", got, want)
	}
	if slices.Contains(commandTexts(groupCommands()), "players") {
		t.Error("/players stays a command but is not in the menu")
	}
}

func TestAdminMenuExtendsGroupMenu(t *testing.T) {
	got := commandTexts(adminCommands())
	want := append(commandTexts(groupCommands()), "settings", "removeplayer", "backup")
	if !reflect.DeepEqual(got, want) {
		t.Errorf("admin menu = %v, want %v", got, want)
	}
}

func TestPrivateMenu(t *testing.T) {
	want := []string{"help", "learn"}
	if got := commandTexts(privateCommands()); !reflect.DeepEqual(got, want) {
		t.Errorf("private menu = %v, want %v", got, want)
	}
}

func TestCommandMenusScopes(t *testing.T) {
	byScope := map[string][]string{}
	for _, m := range commandMenus() {
		byScope[m.scope.Type] = commandTexts(m.cmds)
	}
	for scope, want := range map[string][]string{
		tele.CommandScopeAllPrivateChats: commandTexts(privateCommands()),
		tele.CommandScopeAllGroupChats:   commandTexts(groupCommands()),
		tele.CommandScopeAllChatAdmin:    commandTexts(adminCommands()),
	} {
		if !reflect.DeepEqual(byScope[scope], want) {
			t.Errorf("scope %s = %v, want %v", scope, byScope[scope], want)
		}
	}
}

// Telegram rejects the whole setMyCommands call on one bad entry.
func TestMenuCommandsAreValid(t *testing.T) {
	textRx := regexp.MustCompile(`^[a-z0-9_]{1,32}$`)
	for _, m := range commandMenus() {
		for _, c := range m.cmds {
			if !textRx.MatchString(c.Text) {
				t.Errorf("%s: bad command text %q", m.scope.Type, c.Text)
			}
			if n := len([]rune(c.Description)); n < 3 || n > 256 {
				t.Errorf("%s: /%s description length %d", m.scope.Type, c.Text, n)
			}
			if !slices.Contains(knownCommands, c.Text) {
				t.Errorf("%s: /%s is not a routed command", m.scope.Type, c.Text)
			}
		}
	}
}
