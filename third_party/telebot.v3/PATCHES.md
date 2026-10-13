# Local patches to telebot v3.3.8

This is gopkg.in/telebot.v3 v3.3.8 copied from the module cache (LICENSE kept),
wired in by `replace gopkg.in/telebot.v3 => ./third_party/telebot.v3` in the root
go.mod. No released telebot (v3.4.x betas, v4 betas) knows the Bot API 10.x
fields below, so they are added here. Only additions, no behaviour changes.

| File | Change |
|------|--------|
| message.go | `Message.EphemeralMessageID` (`ephemeral_message_id`) and `Message.ReceiverUser` (`receiver_user`). An ephemeral message arrives with `message_id = 0`. |
| commands.go | `Command.IsEphemeral` (`is_ephemeral`): the command is sent invisibly to the rest of the chat. |
| markup.go | `InlineButton.Style` (`style`: danger, success, primary) and `InlineButton.Disabled` (`disabled`, `*struct{}` that marshals to `{}`). The same two fields on `Btn`, copied over in `Btn.Inline()`. |

Checked: `ProcessUpdate` routes messages and commands by text, never by
`message_id`, so an ephemeral command with `message_id = 0` reaches its handler.

Sending, editing and deleting ephemeral messages goes through `Bot.Raw` in the
bot itself (internal/bot/tgx.go), not through this package.

To upgrade telebot: copy the new version here, reapply the table above, keep
this file.
