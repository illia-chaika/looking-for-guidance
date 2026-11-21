package main

import (
	"fmt"
	"strings"
	"sync"
)

// The companion's own menu follows the system language, in the eleven languages the add-on speaks.
// Texts that travel into the game are translated there, by the add-on.
var menuLanguages = []string{"ru", "de", "fr", "es", "pt", "ko", "zh-Hans", "zh-Hant", "it", "uk"}

// English is the key; the translations follow the order of menuLanguages.
var menuTexts = map[string][10]string{
	"Waiting for WoW Forever to start": {
		"Ожидаю запуска WoW Forever", "Warte auf den Start von WoW Forever", "En attente du lancement de WoW Forever",
		"Esperando a que se inicie WoW Forever", "Aguardando o WoW Forever iniciar", "WoW Forever 실행을 기다리는 중",
		"正在等待 WoW Forever 启动", "正在等待 WoW Forever 啟動",
		"In attesa dell’avvio di WoW Forever", "Чекаю на запуск WoW Forever"},
	"Start with WoW": {
		"Запускать вместе с WoW", "Mit WoW starten", "Lancer avec WoW", "Iniciar con WoW", "Iniciar com o WoW",
		"WoW와 함께 시작", "随 WoW 启动", "隨 WoW 啟動",
		"Avvia con WoW", "Запускати разом із WoW"},
	"Start the companion together with the game": {
		"Запускать помощника вместе с игрой", "Companion zusammen mit dem Spiel starten",
		"Lancer le compagnon en même temps que le jeu", "Iniciar el compañero junto con el juego",
		"Iniciar o companheiro junto com o jogo", "게임과 함께 도우미를 시작합니다", "与游戏一同启动助手", "與遊戲一同啟動助手",
		"Avvia l’app compagna insieme al gioco", "Запускати помічника разом із грою"},
	"Choose the WoW folder…": {
		"Выбрать папку WoW…", "WoW-Ordner auswählen…", "Choisir le dossier de WoW…", "Elegir la carpeta de WoW…",
		"Escolher a pasta do WoW…", "WoW 폴더 선택…", "选择 WoW 文件夹…", "選擇 WoW 資料夾…",
		"Scegli la cartella di WoW…", "Вибрати теку WoW…"},
	"Retry access to the WoW folder": {
		"Повторить доступ к папке WoW", "Zugriff auf den WoW-Ordner erneut versuchen",
		"Réessayer l’accès au dossier de WoW", "Reintentar el acceso a la carpeta de WoW",
		"Tentar acessar a pasta do WoW novamente", "WoW 폴더 접근 다시 시도", "重试访问 WoW 文件夹", "重試存取 WoW 資料夾",
		"Riprova ad accedere alla cartella di WoW", "Повторити доступ до теки WoW"},
	"Quit Looking for Guidance": {
		"Закрыть Looking for Guidance", "Looking for Guidance beenden", "Quitter Looking for Guidance",
		"Salir de Looking for Guidance", "Encerrar o Looking for Guidance", "Looking for Guidance 종료",
		"退出 Looking for Guidance", "結束 Looking for Guidance",
		"Esci da Looking for Guidance", "Закрити Looking for Guidance"},
	"No access to the WoW folder": {
		"Нет доступа к папке WoW", "Kein Zugriff auf den WoW-Ordner", "Pas d’accès au dossier de WoW",
		"Sin acceso a la carpeta de WoW", "Sem acesso à pasta do WoW", "WoW 폴더에 접근할 수 없음",
		"无法访问 WoW 文件夹", "無法存取 WoW 資料夾",
		"Nessun accesso alla cartella di WoW", "Немає доступу до теки WoW"},
	"Check the disk permission and choose “Retry access”.": {
		"Проверь разрешение на диск и нажми «Повторить доступ».",
		"Prüfe die Berechtigung für das Laufwerk und wähle „Zugriff erneut versuchen“.",
		"Vérifie l’autorisation du disque, puis choisis « Réessayer l’accès ».",
		"Comprueba el permiso del disco y elige «Reintentar el acceso».",
		"Verifique a permissão do disco e escolha “Tentar acessar novamente”.",
		"디스크 권한을 확인한 뒤 ‘접근 다시 시도’를 선택하세요.", "请检查磁盘权限，然后选择“重试访问”。", "請檢查磁碟權限，然後選擇「重試存取」。",
		"Controlla il permesso per il disco e scegli «Riprova ad accedere alla cartella di WoW».", "Перевір дозвіл на диск і натисни «Повторити доступ»."},
	"Choose the WoW Forever folder": {
		"Выбери папку WoW Forever", "Wähle den Ordner von WoW Forever", "Choisis le dossier de WoW Forever",
		"Elige la carpeta de WoW Forever", "Escolha a pasta do WoW Forever", "WoW Forever 폴더를 선택하세요",
		"请选择 WoW Forever 文件夹", "請選擇 WoW Forever 資料夾",
		"Scegli la cartella di WoW Forever", "Вибери теку WoW Forever"},
	"Restart WoW to load the add-on": {
		"Перезапусти WoW, чтобы загрузить аддон", "Starte WoW neu, um das Addon zu laden",
		"Relance WoW pour charger l’addon", "Reinicia WoW para cargar el addon", "Reinicie o WoW para carregar o addon",
		"애드온을 불러오려면 WoW를 다시 시작하세요", "请重新启动 WoW 以载入插件", "請重新啟動 WoW 以載入插件",
		"Riavvia WoW per caricare l’add-on", "Перезапусти WoW, щоб завантажити аддон"},
	"Waiting for a character to enter the game": {
		"Жду входа персонажа в игру", "Warte darauf, dass ein Charakter das Spiel betritt",
		"En attente de l’entrée d’un personnage en jeu", "Esperando a que un personaje entre en el juego",
		"Aguardando um personagem entrar no jogo", "캐릭터가 게임에 접속하기를 기다리는 중", "正在等待角色进入游戏", "正在等待角色進入遊戲",
		"In attesa che un personaggio entri in gioco", "Чекаю, поки персонаж увійде в гру"},
	"Connected to the game": {
		"Связь с игрой установлена", "Mit dem Spiel verbunden", "Connecté au jeu", "Conectado con el juego",
		"Conectado ao jogo", "게임과 연결됨", "已与游戏连接", "已與遊戲連線",
		"Collegato al gioco", "Зв’язок із грою встановлено"},
	"Chats: %d": {"Чатов: %d", "Chats: %d", "Discussions : %d", "Chats: %d", "Conversas: %d", "대화: %d", "对话：%d", "對話：%d",
		"Chat: %d", "Чатів: %d"},
	"Couldn't check the add-on: %s": {
		"Не удалось проверить аддон: %s", "Das Addon konnte nicht geprüft werden: %s",
		"Impossible de vérifier l’addon : %s", "No se pudo comprobar el addon: %s",
		"Não foi possível verificar o addon: %s", "애드온을 확인하지 못했습니다: %s", "无法检查插件：%s", "無法檢查插件：%s",
		"Impossibile controllare l’add-on: %s", "Не вдалося перевірити аддон: %s"},
	"Adviser": {"Советник", "Berater", "Conseiller", "Consejero", "Conselheiro", "조언자", "顾问", "顧問",
		"Consigliere", "Порадник"},
	"%s was not found": {
		"Не удалось найти %s", "%s wurde nicht gefunden", "%s est introuvable", "No se encontró %s",
		"%s não foi encontrado", "%s을(를) 찾을 수 없음", "未找到 %s", "找不到 %s",
		"%s non trovato", "Не вдалося знайти %s"},
	"Install %s…": {
		"Установить %s…", "%s installieren…", "Installer %s…", "Instalar %s…", "Instalar %s…", "%s 설치…",
		"安装 %s…", "安裝 %s…",
		"Installa %s…", "Встановити %s…"},
	"Codex is not signed in to ChatGPT": {
		"Codex не вошёл в ChatGPT", "Codex ist nicht bei ChatGPT angemeldet", "Codex n’est pas connecté à ChatGPT",
		"Codex no ha iniciado sesión en ChatGPT", "O Codex não está conectado ao ChatGPT",
		"Codex가 ChatGPT에 로그인되어 있지 않음", "Codex 尚未登录 ChatGPT", "Codex 尚未登入 ChatGPT",
		"Codex non ha effettuato l’accesso a ChatGPT", "Codex не ввійшов у ChatGPT"},
	// The add-on quotes these words in its own note about signing in (SmartStrings.lua).
	"Sign in to ChatGPT…": {
		"Войти в ChatGPT…", "Bei ChatGPT anmelden…", "Se connecter à ChatGPT…", "Iniciar sesión en ChatGPT…",
		"Entrar no ChatGPT…", "ChatGPT에 로그인…", "登录 ChatGPT…", "登入 ChatGPT…",
		"Accedi a ChatGPT…", "Увійти в ChatGPT…"},
	"No adviser was found": {
		"Не найден ни один советник", "Kein Berater gefunden", "Aucun conseiller trouvé",
		"No se encontró ningún consejero", "Nenhum conselheiro encontrado", "조언자를 찾을 수 없음",
		"未找到任何顾问", "找不到任何顧問",
		"Nessun consigliere trovato", "Не знайдено жодного порадника"},
	"Claude Code is not signed in": {
		"Claude Code не вошёл в аккаунт", "Claude Code ist nicht angemeldet", "Claude Code n’est pas connecté",
		"Claude Code no ha iniciado sesión", "O Claude Code não está conectado", "Claude Code에 로그인되어 있지 않음",
		"Claude Code 尚未登录", "Claude Code 尚未登入",
		"Claude Code non ha effettuato l’accesso", "Claude Code не ввійшов в обліковий запис"},
	"Sign in to Claude Code…": {
		"Войти в Claude Code…", "Bei Claude Code anmelden…", "Se connecter à Claude Code…", "Iniciar sesión en Claude Code…",
		"Entrar no Claude Code…", "Claude Code에 로그인…", "登录 Claude Code…", "登入 Claude Code…",
		"Accedi a Claude Code…", "Увійти в Claude Code…"},
}

var menuLanguage = sync.OnceValue(func() string {
	if chosen := loadSettings().Language; chosen != "" {
		return matchLanguage([]string{chosen})
	}
	return matchLanguage(systemLanguages())
})

// matchLanguage picks the first preferred language the menu has, as the system does for an app's own texts.
func matchLanguage(preferred []string) string {
	for _, tag := range preferred {
		tag = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(tag), "_", "-"))
		tag, _, _ = strings.Cut(tag, ".") // ru_RU.UTF-8
		base, rest, _ := strings.Cut(tag, "-")
		switch base {
		case "en":
			return "en"
		case "zh":
			if strings.Contains(rest, "hant") || strings.HasPrefix(rest, "tw") || strings.HasPrefix(rest, "hk") || strings.HasPrefix(rest, "mo") {
				return "zh-Hant"
			}
			return "zh-Hans"
		}
		for _, have := range menuLanguages {
			if base == have {
				return have
			}
		}
	}
	return "en"
}

// tr returns the menu text in the system language. The English text is its own key.
func tr(text string, args ...any) string {
	return trIn(menuLanguage(), text, args...)
}

func trIn(language, text string, args ...any) string {
	if translations, ok := menuTexts[text]; ok {
		for i, have := range menuLanguages {
			if have == language {
				text = translations[i]
			}
		}
	}
	if len(args) > 0 {
		return fmt.Sprintf(text, args...)
	}
	return text
}
