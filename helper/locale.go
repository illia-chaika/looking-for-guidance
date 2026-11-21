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
	"Cancel": {"Отмена", "Abbrechen", "Annuler", "Cancelar", "Cancelar", "취소", "取消", "取消", "Annulla", "Скасувати"},
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
