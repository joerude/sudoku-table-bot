package storage

import "testing"

func TestSetPhrase(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-311)
	st.EnsureChat(chat, 1)
	p, _, _ := st.RegisterPlayer(chat, 1, "Вова")
	if got, err := st.Phrase(p.ID); err != nil || got != "" {
		t.Fatalf("fresh player: Phrase = (%q, %v), want empty", got, err)
	}
	if err := st.SetPhrase(p.ID, "Нагнул тебя брат"); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Phrase(p.ID); got != "Нагнул тебя брат" {
		t.Errorf("Phrase = %q after set", got)
	}
	if err := st.SetPhrase(p.ID, ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Phrase(p.ID); got != "" {
		t.Errorf("Phrase = %q after clear, want empty", got)
	}
}

func TestSetPhraseGifKeepsPhrase(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-312)
	st.EnsureChat(chat, 1)
	p, _, _ := st.RegisterPlayer(chat, 1, "Вова")
	_ = st.SetPhrase(p.ID, "Нагнул тебя брат")
	if err := st.SetPhraseGif(p.ID, "CgACAgIAAxkB"); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.PhraseGif(p.ID); got != "CgACAgIAAxkB" {
		t.Errorf("PhraseGif = %q after set", got)
	}
	_ = st.SetPhraseGif(p.ID, "")
	if got, _ := st.PhraseGif(p.ID); got != "" {
		t.Errorf("PhraseGif = %q after clear, want empty", got)
	}
	if got, _ := st.Phrase(p.ID); got != "Нагнул тебя брат" {
		t.Errorf("Phrase = %q, the GIF must not touch it", got)
	}
}

func TestSetPhraseAudioKeepsGif(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-313)
	st.EnsureChat(chat, 1)
	p, _, _ := st.RegisterPlayer(chat, 1, "Вова")
	_ = st.SetPhraseGif(p.ID, "CgACAgIAAxkB")
	if err := st.SetPhraseAudio(p.ID, "voice:AwACAgIAAxkB"); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.PhraseAudio(p.ID); got != "voice:AwACAgIAAxkB" {
		t.Errorf("PhraseAudio = %q after set", got)
	}
	_ = st.SetPhraseAudio(p.ID, "")
	if got, _ := st.PhraseAudio(p.ID); got != "" {
		t.Errorf("PhraseAudio = %q after clear, want empty", got)
	}
	if got, _ := st.PhraseGif(p.ID); got != "CgACAgIAAxkB" {
		t.Errorf("PhraseGif = %q, the audio must not touch it", got)
	}
}
