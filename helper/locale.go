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
	"Check for Updates…": {
		"Проверить обновления…", "Nach Updates suchen…", "Rechercher des mises à jour…", "Buscar actualizaciones…",
		"Procurar atualizações…", "업데이트 확인…", "检查更新…", "檢查更新…",
		"Controlla aggiornamenti…", "Перевірити оновлення…"},
	"Checking for Updates…": {
		"Проверяю обновления…", "Suche nach Updates…", "Recherche de mises à jour…", "Buscando actualizaciones…",
		"Procurando atualizações…", "업데이트 확인 중…", "正在检查更新…", "正在檢查更新…",
		"Controllo degli aggiornamenti…", "Перевіряю оновлення…"},
	"Looking for Guidance %s is up to date": {
		"Looking for Guidance %s — последняя версия", "Looking for Guidance %s ist aktuell",
		"Looking for Guidance %s est à jour", "Looking for Guidance %s está actualizado",
		"O Looking for Guidance %s está atualizado", "Looking for Guidance %s: 최신 버전",
		"Looking for Guidance %s 已是最新版本", "Looking for Guidance %s 已是最新版本",
		"Looking for Guidance %s è aggiornato", "Looking for Guidance %s — остання версія"},
	"Downloading Looking for Guidance %s…": {
		"Загружаю Looking for Guidance %s…", "Lade Looking for Guidance %s herunter…",
		"Téléchargement de Looking for Guidance %s…", "Descargando Looking for Guidance %s…",
		"Baixando o Looking for Guidance %s…", "Looking for Guidance %s 다운로드 중…",
		"正在下载 Looking for Guidance %s…", "正在下載 Looking for Guidance %s…",
		"Download di Looking for Guidance %s…", "Завантажую Looking for Guidance %s…"},
	"Install %s and Restart": {
		"Установить %s и перезапустить", "%s installieren und neu starten", "Installer %s et redémarrer",
		"Instalar %s y reiniciar", "Instalar %s e reiniciar", "%s 설치 후 다시 시작", "安装 %s 并重新启动", "安裝 %s 並重新啟動",
		"Installa %s e riavvia", "Встановити %s і перезапустити"},
	"Installing Looking for Guidance %s…": {
		"Устанавливаю Looking for Guidance %s…", "Installiere Looking for Guidance %s…",
		"Installation de Looking for Guidance %s…", "Instalando Looking for Guidance %s…",
		"Instalando o Looking for Guidance %s…", "Looking for Guidance %s 설치 중…",
		"正在安装 Looking for Guidance %s…", "正在安裝 Looking for Guidance %s…",
		"Installazione di Looking for Guidance %s…", "Встановлюю Looking for Guidance %s…"},
	"Update Failed: Download %s from GitHub…": {
		"Обновление не удалось: скачать %s с GitHub…", "Update fehlgeschlagen: %s von GitHub laden…",
		"Échec de la mise à jour : télécharger %s depuis GitHub…", "La actualización falló: descargar %s de GitHub…",
		"A atualização falhou: baixar %s do GitHub…", "업데이트 실패: GitHub에서 %s 다운로드…",
		"更新失败：从 GitHub 下载 %s…", "更新失敗：從 GitHub 下載 %s…",
		"Aggiornamento non riuscito: scarica %s da GitHub…", "Оновлення не вдалося: завантажити %s з GitHub…"},
	"Own Battle.net API client": {
		"Свой API-клиент Battle.net",
		"Eigener Battle.net-API-Client",
		"Client API Battle.net personnel",
		"Cliente propio de la API de Battle.net",
		"Cliente próprio da API da Battle.net",
		"내 Battle.net API 클라이언트",
		"自己的 Battle.net API 客户端",
		"自己的 Battle.net API 用戶端",
		"Client API di Battle.net personale",
		"Власний API-клієнт Battle.net"},
	"Auction prices come from the project's mirror; with this on, the companion asks Blizzard with your own API client instead": {
		"Цены аукциона приходят с зеркала проекта; с этим пунктом помощник спрашивает Blizzard через твой собственный API-клиент",
		"Auktionspreise kommen vom Spiegel des Projekts; ist dies an, fragt der Companion Blizzard mit deinem eigenen API-Client",
		"Les prix de l’hôtel des ventes viennent du miroir du projet ; avec cette option, le compagnon interroge Blizzard avec ton propre client API",
		"Los precios de subasta vienen del espejo del proyecto; con esto activado, el compañero consulta a Blizzard con tu propio cliente de la API",
		"Os preços do leilão vêm do espelho do projeto; com isto ativado, o companheiro consulta a Blizzard com o seu próprio cliente da API",
		"경매장 시세는 프로젝트 미러에서 받아 옵니다. 이 항목을 켜면 도우미가 내 API 클라이언트로 Blizzard에 직접 요청합니다",
		"拍卖行价格来自项目的镜像；开启后，助手会用你自己的 API 客户端直接向 Blizzard 查询",
		"拍賣場價格來自專案的鏡像；開啟後，助手會用你自己的 API 用戶端直接向 Blizzard 查詢",
		"I prezzi d’asta arrivano dal mirror del progetto; con questa voce attiva, l’app compagna interroga Blizzard con il tuo client API",
		"Ціни аукціону надходять із дзеркала проєкту; з цим пунктом помічник звертається до Blizzard через твій власний API-клієнт"},
	"Auction prices already come from the project's mirror. To ask Blizzard with your own free Battle.net API client instead, create one on the page that has just opened, then paste its Client ID here.": {
		"Цены аукциона и так приходят с зеркала проекта. Чтобы спрашивать Blizzard через свой бесплатный API-клиент Battle.net, создай его на открывшейся странице и вставь сюда его Client ID.",
		"Auktionspreise kommen bereits vom Spiegel des Projekts. Um Blizzard stattdessen mit einem eigenen kostenlosen Battle.net-API-Client zu fragen, erstelle ihn auf der gerade geöffneten Seite und füge hier seine Client-ID ein.",
		"Les prix de l’hôtel des ventes viennent déjà du miroir du projet. Pour interroger Blizzard avec ton propre client API Battle.net, gratuit, crée-le sur la page qui vient de s’ouvrir, puis colle ici son Client ID.",
		"Los precios de subasta ya llegan del espejo del proyecto. Para consultar a Blizzard con tu propio cliente gratuito de la API de Battle.net, créalo en la página que se acaba de abrir y pega aquí su Client ID.",
		"Os preços do leilão já vêm do espelho do projeto. Para consultar a Blizzard com o seu próprio cliente gratuito da API da Battle.net, crie um na página que acabou de abrir e cole aqui o Client ID dele.",
		"경매장 시세는 이미 프로젝트 미러에서 받아 옵니다. 대신 무료 Battle.net API 클라이언트로 Blizzard에 직접 요청하려면 방금 열린 페이지에서 클라이언트를 만든 뒤 Client ID를 여기에 붙여 넣으세요.",
		"拍卖行价格已经从项目的镜像获取。若想改用你自己的免费 Battle.net API 客户端直接向 Blizzard 查询，请在刚打开的页面上创建一个，然后把它的 Client ID 粘贴到这里。",
		"拍賣場價格已經從專案的鏡像取得。若想改用你自己的免費 Battle.net API 用戶端直接向 Blizzard 查詢，請在剛開啟的頁面上建立一個，然後把它的 Client ID 貼到這裡。",
		"I prezzi d’asta arrivano già dal mirror del progetto. Per interrogare Blizzard con un tuo client API di Battle.net, gratuito, crealo nella pagina appena aperta, poi incolla qui il suo Client ID.",
		"Ціни аукціону й так надходять із дзеркала проєкту. Щоб звертатися до Blizzard через власний безкоштовний API-клієнт Battle.net, створи його на сторінці, що відкрилася, і встав сюди його Client ID."},
	"Now paste the client secret from the same page.": {
		"Теперь вставь Client secret с той же страницы.", "Füge jetzt das Client-Secret von derselben Seite ein.",
		"Colle maintenant le Client secret de la même page.", "Ahora pega el Client secret de la misma página.",
		"Agora cole o Client secret da mesma página.", "이제 같은 페이지의 Client secret을 붙여 넣으세요.",
		"现在粘贴同一页面上的 Client secret。", "現在貼上同一頁面上的 Client secret。",
		"Ora incolla il Client secret dalla stessa pagina.", "Тепер встав Client secret із тієї ж сторінки."},
	"Battle.net did not accept this client ID and secret. Check both on develop.battle.net and choose Own Battle.net API client again.": {
		"Battle.net не принял этот Client ID и secret. Проверь оба на develop.battle.net и снова выбери «Свой API-клиент Battle.net».",
		"Battle.net hat diese Client-ID und dieses Secret nicht angenommen. Prüfe beide auf develop.battle.net und wähle erneut „Eigener Battle.net-API-Client“.",
		"Battle.net n’a pas accepté ce Client ID et ce secret. Vérifie-les sur develop.battle.net et choisis à nouveau « Client API Battle.net personnel ».",
		"Battle.net no aceptó este Client ID y este secret. Revisa ambos en develop.battle.net y vuelve a elegir «Cliente propio de la API de Battle.net».",
		"A Battle.net não aceitou este Client ID e este secret. Confira os dois em develop.battle.net e escolha “Cliente próprio da API da Battle.net” novamente.",
		"Battle.net이 이 Client ID와 secret을 받아들이지 않았습니다. develop.battle.net에서 둘 다 확인한 뒤 ‘내 Battle.net API 클라이언트’를 다시 선택하세요.",
		"Battle.net 未接受此 Client ID 和 secret。请在 develop.battle.net 上检查两者，然后再次选择“自己的 Battle.net API 客户端”。",
		"Battle.net 未接受此 Client ID 和 secret。請在 develop.battle.net 上檢查兩者，然後再次選擇「自己的 Battle.net API 用戶端」。",
		"Battle.net non ha accettato questo Client ID e questo secret. Controllali su develop.battle.net e scegli di nuovo «Client API di Battle.net personale».",
		"Battle.net не прийняв цей Client ID і secret. Перевір обидва на develop.battle.net і знову вибери «Власний API-клієнт Battle.net»."},
	"Couldn't reach Battle.net to check the client: %s": {
		"Не удалось связаться с Battle.net, чтобы проверить клиент: %s", "Battle.net war zum Prüfen des Clients nicht erreichbar: %s",
		"Impossible de joindre Battle.net pour vérifier le client : %s", "No se pudo contactar con Battle.net para comprobar el cliente: %s",
		"Não foi possível falar com a Battle.net para verificar o cliente: %s", "클라이언트를 확인하려고 Battle.net에 연결하지 못했습니다: %s",
		"无法连接 Battle.net 检查客户端：%s", "無法連線 Battle.net 檢查用戶端：%s",
		"Impossibile raggiungere Battle.net per controllare il client: %s", "Не вдалося зв’язатися з Battle.net, щоб перевірити клієнт: %s"},
	"Couldn't change the stored client: %s": {
		"Не удалось изменить сохранённый клиент: %s", "Der gespeicherte Client ließ sich nicht ändern: %s",
		"Impossible de modifier le client enregistré : %s", "No se pudo cambiar el cliente guardado: %s",
		"Não foi possível alterar o cliente salvo: %s", "저장된 클라이언트를 변경하지 못했습니다: %s",
		"无法更改已保存的客户端：%s", "無法變更已儲存的用戶端：%s",
		"Impossibile modificare il client salvato: %s", "Не вдалося змінити збережений клієнт: %s"},
	"Your own Battle.net API client is stored. From now on the companion asks Blizzard for your auction house itself.": {
		"Твой API-клиент Battle.net сохранён. Теперь помощник сам спрашивает Blizzard о твоём аукционе.",
		"Dein eigener Battle.net-API-Client ist gespeichert. Ab jetzt fragt der Companion Blizzard selbst nach deinem Auktionshaus.",
		"Ton client API Battle.net est enregistré. Désormais, le compagnon interroge lui-même Blizzard sur ton hôtel des ventes.",
		"Tu cliente de la API de Battle.net está guardado. A partir de ahora el compañero consulta tu casa de subastas directamente a Blizzard.",
		"Seu cliente da API da Battle.net foi salvo. A partir de agora o companheiro consulta sua casa de leilões diretamente na Blizzard.",
		"내 Battle.net API 클라이언트를 저장했습니다. 이제 도우미가 경매장 정보를 Blizzard에 직접 요청합니다.",
		"你的 Battle.net API 客户端已保存。从现在起，助手会直接向 Blizzard 查询你所在的拍卖行。",
		"你的 Battle.net API 用戶端已儲存。從現在起，助手會直接向 Blizzard 查詢你所在的拍賣場。",
		"Il tuo client API di Battle.net è salvato. Da ora l’app compagna chiede direttamente a Blizzard la tua casa d’aste.",
		"Твій API-клієнт Battle.net збережено. Тепер помічник сам звертається до Blizzard щодо твого аукціону."},
	"Stop using your own Battle.net API client? The companion removes it from this computer and reads auction prices from the project's mirror again.": {
		"Перестать использовать свой API-клиент Battle.net? Помощник удалит его с этого компьютера и снова будет брать цены аукциона с зеркала проекта.",
		"Eigenen Battle.net-API-Client nicht mehr verwenden? Der Companion entfernt ihn von diesem Computer und liest Auktionspreise wieder vom Spiegel des Projekts.",
		"Ne plus utiliser ton client API Battle.net ? Le compagnon le retire de cet ordinateur et reprend les prix de l’hôtel des ventes sur le miroir du projet.",
		"¿Dejar de usar tu cliente de la API de Battle.net? El compañero lo quitará de este equipo y volverá a leer los precios de subasta del espejo del proyecto.",
		"Parar de usar seu cliente da API da Battle.net? O companheiro o remove deste computador e volta a ler os preços do leilão no espelho do projeto.",
		"내 Battle.net API 클라이언트를 그만 쓸까요? 도우미가 이 컴퓨터에서 클라이언트를 지우고 다시 프로젝트 미러에서 경매장 시세를 읽습니다.",
		"停止使用你自己的 Battle.net API 客户端？助手会从这台电脑上移除它，并重新从项目的镜像读取拍卖行价格。",
		"停止使用你自己的 Battle.net API 用戶端？助手會從這台電腦上移除它，並重新從專案的鏡像讀取拍賣場價格。",
		"Smettere di usare il tuo client API di Battle.net? L’app compagna lo rimuove da questo computer e torna a leggere i prezzi d’asta dal mirror del progetto.",
		"Перестати використовувати власний API-клієнт Battle.net? Помічник видалить його з цього комп’ютера й знову братиме ціни аукціону з дзеркала проєкту."},
	"Sign in with Battle.net…": {
		"Войти через Battle.net…",
		"Mit Battle.net anmelden…",
		"Se connecter avec Battle.net…",
		"Iniciar sesión con Battle.net…",
		"Entrar com a Battle.net…",
		"Battle.net으로 로그인…",
		"使用 Battle.net 登录…",
		"使用 Battle.net 登入…",
		"Accedi con Battle.net…",
		"Увійти через Battle.net…"},
	"Signed in with Battle.net": {
		"Вход через Battle.net выполнен",
		"Mit Battle.net angemeldet",
		"Connecté avec Battle.net",
		"Sesión iniciada con Battle.net",
		"Conectado com a Battle.net",
		"Battle.net으로 로그인됨",
		"已使用 Battle.net 登录",
		"已使用 Battle.net 登入",
		"Accesso con Battle.net effettuato",
		"Вхід через Battle.net виконано"},
	"Auction prices and the world database come from the project's server, which Blizzard's terms let it share only with the app's players. The sign-in only shows that you have a Battle.net account. Nothing about it is kept.": {
		"Цены аукциона и база мира приходят с сервера проекта, а условия Blizzard разрешают делиться ими только с игроками приложения. Вход лишь показывает, что у тебя есть аккаунт Battle.net. Ничего о нём не сохраняется.",
		"Auktionspreise und die Weltdatenbank kommen vom Server des Projekts, und Blizzards Bedingungen erlauben, sie nur mit den Spielern der App zu teilen. Die Anmeldung zeigt nur, dass du ein Battle.net-Konto hast. Nichts darüber wird gespeichert.",
		"Les prix de l’hôtel des ventes et la base du monde viennent du serveur du projet, que les conditions de Blizzard autorisent à les partager seulement avec les joueurs de l’app. La connexion montre seulement que tu as un compte Battle.net. Rien n’en est conservé.",
		"Los precios de subasta y la base de datos del mundo vienen del servidor del proyecto, y las condiciones de Blizzard solo le permiten compartirlos con los jugadores de la app. El inicio de sesión solo muestra que tienes una cuenta de Battle.net. No se guarda nada de ella.",
		"Os preços do leilão e o banco de dados do mundo vêm do servidor do projeto, e os termos da Blizzard só permitem compartilhá-los com os jogadores do app. A entrada só mostra que você tem uma conta Battle.net. Nada sobre ela é guardado.",
		"경매장 시세와 월드 데이터베이스는 프로젝트 서버에서 받아 오며, Blizzard 약관상 앱 사용자에게만 제공할 수 있습니다. 로그인은 Battle.net 계정이 있다는 사실만 확인하며, 계정 정보는 저장하지 않습니다.",
		"拍卖行价格和世界数据库来自项目的服务器，而 Blizzard 的条款只允许与本应用的玩家共享这些数据。登录只证明你有 Battle.net 账号，不会保存任何账号信息。",
		"拍賣場價格和世界資料庫來自專案的伺服器，而 Blizzard 的條款只允許與本應用程式的玩家共享這些資料。登入只證明你有 Battle.net 帳號，不會保存任何帳號資訊。",
		"I prezzi d’asta e il database del mondo arrivano dal server del progetto, e i termini di Blizzard permettono di condividerli solo con i giocatori dell’app. L’accesso mostra soltanto che hai un account Battle.net. Non ne viene conservato nulla.",
		"Ціни аукціону й база світу надходять із сервера проєкту, а умови Blizzard дозволяють ділитися ними лише з гравцями застосунку. Вхід лише показує, що в тебе є акаунт Battle.net. Нічого про нього не зберігається."},
	"Auction prices and the world database come from the project's server. Blizzard's terms let the project share this data only with its own players, so the server asks for a one-time sign-in with Battle.net.\n\nBlizzard's sign-in page opens in your browser. It will say the app may see your Account ID and BattleTag: Blizzard shows that for every app that uses its sign-in. The server never reads them, does not ask for your e-mail or characters, and keeps nothing about your account. It only learns that the sign-in worked. Continue?": {
		"Цены аукциона и база мира приходят с сервера проекта. Условия Blizzard разрешают проекту делиться этими данными только со своими игроками, поэтому сервер просит один раз войти через Battle.net.\n\nСтраница входа Blizzard откроется в браузере. На ней будет сказано, что приложение может увидеть ID аккаунта и BattleTag: Blizzard пишет так для любого приложения со входом через Battle.net. Сервер их не читает, не запрашивает почту и персонажей и ничего о твоём аккаунте не хранит — он узнаёт только, что вход удался. Продолжить?",
		"Auktionspreise und die Weltdatenbank kommen vom Server des Projekts. Blizzards Bedingungen erlauben dem Projekt, diese Daten nur mit seinen eigenen Spielern zu teilen, deshalb bittet der Server um eine einmalige Anmeldung mit Battle.net.\n\nBlizzards Anmeldeseite öffnet sich im Browser. Dort steht, dass die App deine Konto-ID und deinen BattleTag sehen darf: Das zeigt Blizzard bei jeder App mit Battle.net-Anmeldung. Der Server liest sie nicht, fragt weder E-Mail noch Charaktere ab und speichert nichts über dein Konto. Er erfährt nur, dass die Anmeldung geklappt hat. Fortfahren?",
		"Les prix de l’hôtel des ventes et la base du monde viennent du serveur du projet. Les conditions de Blizzard ne permettent au projet de partager ces données qu’avec ses propres joueurs : le serveur demande donc une connexion unique avec Battle.net.\n\nLa page de connexion de Blizzard s’ouvre dans ton navigateur. Elle indiquera que l’app peut voir l’ID de ton compte et ton BattleTag : Blizzard l’affiche pour toute app qui utilise sa connexion. Le serveur ne les lit pas, ne demande ni ton e-mail ni tes personnages et ne conserve rien sur ton compte. Il apprend seulement que la connexion a réussi. Continuer ?",
		"Los precios de subasta y la base de datos del mundo vienen del servidor del proyecto. Las condiciones de Blizzard solo permiten al proyecto compartir estos datos con sus propios jugadores, así que el servidor pide iniciar sesión una sola vez con Battle.net.\n\nLa página de inicio de sesión de Blizzard se abre en tu navegador. Dirá que la app puede ver el ID de tu cuenta y tu BattleTag: Blizzard lo muestra para cualquier app que use su inicio de sesión. El servidor no los lee, no pide tu correo ni tus personajes y no guarda nada de tu cuenta. Solo sabe que el inicio de sesión funcionó. ¿Continuar?",
		"Os preços do leilão e o banco de dados do mundo vêm do servidor do projeto. Os termos da Blizzard só permitem ao projeto compartilhar esses dados com os próprios jogadores, então o servidor pede uma única entrada com a Battle.net.\n\nA página de entrada da Blizzard abre no seu navegador. Ela vai dizer que o app pode ver o ID da sua conta e o seu BattleTag: a Blizzard mostra isso para qualquer app que use a entrada dela. O servidor não os lê, não pede seu e-mail nem seus personagens e não guarda nada sobre a sua conta. Ele só fica sabendo que a entrada deu certo. Continuar?",
		"경매장 시세와 월드 데이터베이스는 프로젝트 서버에서 받아 옵니다. Blizzard 약관상 프로젝트는 이 데이터를 자체 사용자에게만 제공할 수 있으므로, 서버는 Battle.net 로그인을 한 번 요청합니다.\n\n브라우저에서 Blizzard 로그인 페이지가 열립니다. 페이지에는 앱이 계정 ID와 BattleTag를 볼 수 있다고 표시되는데, Battle.net 로그인을 쓰는 모든 앱에 똑같이 표시되는 문구입니다. 서버는 이 정보를 읽지 않고, 이메일이나 캐릭터도 요청하지 않으며, 계정에 관한 어떤 정보도 저장하지 않습니다. 로그인에 성공했다는 사실만 알게 됩니다. 계속할까요?",
		"拍卖行价格和世界数据库来自项目的服务器。Blizzard 的条款只允许项目与自己的玩家共享这些数据，因此服务器需要你用 Battle.net 登录一次。\n\n浏览器中会打开 Blizzard 的登录页面。页面上会写着应用可以查看你的账号 ID 和 BattleTag：凡是使用 Battle.net 登录的应用，Blizzard 都会这样显示。服务器不会读取这些信息，不会请求你的电子邮件或角色，也不会保存任何账号信息。它只知道登录成功了。继续吗？",
		"拍賣場價格和世界資料庫來自專案的伺服器。Blizzard 的條款只允許專案與自己的玩家共享這些資料，因此伺服器需要你用 Battle.net 登入一次。\n\n瀏覽器中會開啟 Blizzard 的登入頁面。頁面上會寫著應用程式可以查看你的帳號 ID 和 BattleTag：凡是使用 Battle.net 登入的應用程式，Blizzard 都會這樣顯示。伺服器不會讀取這些資訊，不會請求你的電子郵件或角色，也不會保存任何帳號資訊。它只知道登入成功了。要繼續嗎？",
		"I prezzi d’asta e il database del mondo arrivano dal server del progetto. I termini di Blizzard permettono al progetto di condividere questi dati solo con i propri giocatori, quindi il server chiede un accesso una tantum con Battle.net.\n\nLa pagina di accesso di Blizzard si apre nel browser. Dirà che l’app può vedere l’ID del tuo account e il tuo BattleTag: Blizzard lo mostra per ogni app che usa il suo accesso. Il server non li legge, non chiede e-mail né personaggi e del tuo account non conserva nulla. Sa soltanto che l’accesso è riuscito. Continuare?",
		"Ціни аукціону й база світу надходять із сервера проєкту. Умови Blizzard дозволяють проєкту ділитися цими даними лише зі своїми гравцями, тож сервер просить один раз увійти через Battle.net.\n\nСторінка входу Blizzard відкриється в браузері. На ній буде сказано, що застосунок може бачити ID акаунта й BattleTag: Blizzard пише так для будь-якого застосунку зі входом через Battle.net. Сервер їх не читає, не запитує пошту й персонажів і нічого про твій акаунт не зберігає — він дізнається лише, що вхід вдався. Продовжити?"},
	"Sign out of Battle.net? The companion forgets its key, and auction prices and world data stop coming from the project's server until you sign in again.": {
		"Выйти из Battle.net? Помощник забудет свой ключ, и цены аукциона и база мира перестанут приходить с сервера проекта, пока ты снова не войдёшь.",
		"Von Battle.net abmelden? Der Companion vergisst seinen Schlüssel, und Auktionspreise und Weltdaten kommen nicht mehr vom Server des Projekts, bis du dich wieder anmeldest.",
		"Se déconnecter de Battle.net ? Le compagnon oublie sa clé, et les prix de l’hôtel des ventes et les données du monde ne viennent plus du serveur du projet jusqu’à ta prochaine connexion.",
		"¿Cerrar sesión de Battle.net? El compañero olvida su clave, y los precios de subasta y los datos del mundo dejan de llegar del servidor del proyecto hasta que vuelvas a iniciar sesión.",
		"Sair da Battle.net? O companheiro esquece a chave dele, e os preços do leilão e os dados do mundo deixam de vir do servidor do projeto até você entrar de novo.",
		"Battle.net에서 로그아웃할까요? 도우미가 키를 지우며, 다시 로그인할 때까지 경매장 시세와 월드 데이터를 프로젝트 서버에서 받아 오지 않습니다.",
		"要退出 Battle.net 吗？助手会删除它的密钥，在你再次登录之前，拍卖行价格和世界数据将不再从项目的服务器获取。",
		"要登出 Battle.net 嗎？助手會刪除它的金鑰，在你再次登入之前，拍賣場價格和世界資料將不再從專案的伺服器取得。",
		"Uscire da Battle.net? L’app compagna dimentica la sua chiave, e prezzi d’asta e dati del mondo non arrivano più dal server del progetto finché non accedi di nuovo.",
		"Вийти з Battle.net? Помічник забуде свій ключ, і ціни аукціону та база світу перестануть надходити із сервера проєкту, доки ти знову не ввійдеш."},
	"Couldn't remove the Battle.net sign-in: %s": {
		"Не удалось убрать вход через Battle.net: %s",
		"Die Battle.net-Anmeldung konnte nicht entfernt werden: %s",
		"Impossible de retirer la connexion Battle.net : %s",
		"No se pudo quitar el inicio de sesión de Battle.net: %s",
		"Não foi possível remover a entrada da Battle.net: %s",
		"Battle.net 로그인을 지우지 못했습니다: %s",
		"无法移除 Battle.net 登录：%s",
		"無法移除 Battle.net 登入：%s",
		"Impossibile rimuovere l’accesso con Battle.net: %s",
		"Не вдалося прибрати вхід через Battle.net: %s"},
	"Couldn't finish the Battle.net sign-in: %s": {
		"Не удалось завершить вход через Battle.net: %s",
		"Die Anmeldung mit Battle.net konnte nicht abgeschlossen werden: %s",
		"Impossible de terminer la connexion avec Battle.net : %s",
		"No se pudo completar el inicio de sesión con Battle.net: %s",
		"Não foi possível concluir a entrada com a Battle.net: %s",
		"Battle.net 로그인을 완료하지 못했습니다: %s",
		"无法完成 Battle.net 登录：%s",
		"無法完成 Battle.net 登入：%s",
		"Impossibile completare l’accesso con Battle.net: %s",
		"Не вдалося завершити вхід через Battle.net: %s"},
	"The Battle.net sign-in was cancelled. You can close this tab.": {
		"Вход через Battle.net отменён. Эту вкладку можно закрыть.",
		"Die Anmeldung mit Battle.net wurde abgebrochen. Du kannst diesen Tab schließen.",
		"La connexion avec Battle.net a été annulée. Tu peux fermer cet onglet.",
		"Se canceló el inicio de sesión con Battle.net. Puedes cerrar esta pestaña.",
		"A entrada com a Battle.net foi cancelada. Você pode fechar esta aba.",
		"Battle.net 로그인이 취소되었습니다. 이 탭을 닫아도 됩니다.",
		"Battle.net 登录已取消。你可以关闭此标签页。",
		"Battle.net 登入已取消。你可以關閉此分頁。",
		"L’accesso con Battle.net è stato annullato. Puoi chiudere questa scheda.",
		"Вхід через Battle.net скасовано. Цю вкладку можна закрити."},
	"The Battle.net sign-in did not finish. Close this tab and try again from the Looking for Guidance menu.": {
		"Вход через Battle.net не завершился. Закрой эту вкладку и попробуй ещё раз из меню Looking for Guidance.",
		"Die Anmeldung mit Battle.net wurde nicht abgeschlossen. Schließe diesen Tab und versuche es erneut über das Menü von Looking for Guidance.",
		"La connexion avec Battle.net n’a pas abouti. Ferme cet onglet et réessaie depuis le menu de Looking for Guidance.",
		"El inicio de sesión con Battle.net no se completó. Cierra esta pestaña y vuelve a intentarlo desde el menú de Looking for Guidance.",
		"A entrada com a Battle.net não foi concluída. Feche esta aba e tente de novo pelo menu do Looking for Guidance.",
		"Battle.net 로그인이 완료되지 않았습니다. 이 탭을 닫고 Looking for Guidance 메뉴에서 다시 시도하세요.",
		"Battle.net 登录未完成。请关闭此标签页，然后从 Looking for Guidance 菜单重试。",
		"Battle.net 登入未完成。請關閉此分頁，然後從 Looking for Guidance 選單重試。",
		"L’accesso con Battle.net non è stato completato. Chiudi questa scheda e riprova dal menu di Looking for Guidance.",
		"Вхід через Battle.net не завершився. Закрий цю вкладку й спробуй ще раз із меню Looking for Guidance."},
	"Signed in with Battle.net. You can close this tab and go back to the game.": {
		"Вход через Battle.net выполнен. Эту вкладку можно закрыть и вернуться в игру.",
		"Mit Battle.net angemeldet. Du kannst diesen Tab schließen und zum Spiel zurückkehren.",
		"Connecté avec Battle.net. Tu peux fermer cet onglet et retourner au jeu.",
		"Sesión iniciada con Battle.net. Puedes cerrar esta pestaña y volver al juego.",
		"Conectado com a Battle.net. Você pode fechar esta aba e voltar ao jogo.",
		"Battle.net으로 로그인했습니다. 이 탭을 닫고 게임으로 돌아가세요.",
		"已使用 Battle.net 登录。你可以关闭此标签页并回到游戏。",
		"已使用 Battle.net 登入。你可以關閉此分頁並回到遊戲。",
		"Accesso con Battle.net effettuato. Puoi chiudere questa scheda e tornare al gioco.",
		"Вхід через Battle.net виконано. Цю вкладку можна закрити й повернутися до гри."},
	"Cancel": {"Отмена", "Abbrechen", "Annuler", "Cancelar", "Cancelar", "취소", "取消", "取消", "Annulla", "Скасувати"},
}

var menuLanguage = sync.OnceValue(chosenMenuLanguage)

// chosenMenuLanguage is language from settings.json, or else the system's.
func chosenMenuLanguage() string {
	if chosen := loadSettings().Language; chosen != "" {
		return matchLanguage([]string{chosen})
	}
	return matchLanguage(systemLanguages())
}

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
