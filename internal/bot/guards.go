package bot

import "fmt"

// Texts of guards and generic failures, kept in one place so every handler says
// the same thing. The guard* ones are callback toasts (c.Respond): other
// handlers should use them instead of inline literals.
const (
	guardJoinFirst = "Сначала /join"                 // the tapper is not a registered player
	guardGameGone  = "Игра не найдена или закрыта"   // the game is missing, deleted or already finished
	guardFailToast = "⚠️ Не вышло, попробуй ещё раз" // fail() on a button press: toast only
	guardFailText  = "⚠️ Что-то пошло не так. Попробуй ещё раз."
)

const (
	removeWhoText = "Кого убрать? Напиши: /removeplayer Имя\n(смотри /players)"
	noPlayersText = "Пока никто не зарегистрирован. Жми /setnick ТвойНикНаUsdoku"
)

// playerNotFoundText is the reply to /removeplayer with an unknown name.
func playerNotFoundText(name string) string {
	return fmt.Sprintf("Не нашёл игрока: <b>%s</b>. Проверь /players.", esc(name))
}
