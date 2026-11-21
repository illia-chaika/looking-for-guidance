# Looking for Guidance

An in-game adviser for World of Warcraft: Forever. It adds a chat window and a field journal that know your
character — level, talents, quest log, bags, professions — and answer with sources checked against the current game,
not against fifteen-year-old guides.

Every player has typed LFG. This is the other kind: looking for guidance.

Ask it things like *"Where do I hand this quest in?"*, *"Is this staff better than mine?"*, *"What should I sell
before I head to town?"* or *"What can I buy for my chest under 2 gold?"*

> **Status: first release.** A ready-made app for macOS 12 or later on Apple Silicon and Intel, signed and notarized
> by Apple (see [Setup](#setup)). The Windows download is experimental: it has never been run on Windows and is not
> signed. Advice runs on your own Claude or ChatGPT plan, through Claude Code or the Codex CLI.

<p align="center"><img src="docs/chat.png" width="640" alt="The chat window: a question about a chest upgrade, answered with three items from the character's auction house and their prices"></p>

## What it does

**Chat.** Open it with `/ask` (or `/guidance`). Ask in your own language; the adviser reads the parts of your character it needs and
searches current sources before answering. Shift-click items, spells and quests into the message. Each character has
its own conversations and its own memory of decisions ("I am saving for the mount", "keep the linen").

**Auction prices** (optional). With your own free Battle.net API client, set up from the companion's menu (see
[Auction prices](#auction-prices)), the adviser can look up asking prices on your character's auction house before
it tells you to buy or sell something there, and search your market for an upgrade for one equipment slot within
a budget.

**Journal.** Eight sections of advice for your character. What the game itself knows is shown at once; what needs
research is prepared when you press the section's button — or by itself, if you switch research to automatic:

| Section | What you get |
| --- | --- |
| Check-up | Unspent talent points, spells waiting at the trainer, empty slots, worn gear, missing food buff, profession caps |
| What changed | Blizzard's hotfixes that touch your class, professions, zone or quest log — each with the exact quote |
| Route | The next stop for the quests you carry |
| Where to level | Zones and dungeons for your next five levels, with quieter alternatives |
| Quest check | Is this quest bugged, changed, or am I missing something? Verdicts come with a quoted source |
| Bags | What to sell, auction, bank, use or keep, judged by tooltips, your professions and your notes; with auction prices set up, what each item asks for on your market |
| Gold and training | Which ranks are worth buying, what to skip, and whether you will afford the mount |
| Dungeons | Quests to pick up before you enter, where to get them, and tips for a first run |

<p align="center"><img src="docs/journal.png" width="49%" alt="The journal's overview: a suggested next step and all eight sections"> <img src="docs/bags.png" width="49%" alt="The Bags section: what to sell, list on the auction house or bank, with prices"></p>

Every prepared answer shows its sources and when it was checked, and says so when a source is not about Forever.
The pictures are drawn from the add-on's own styles with made-up data, not taken from the game.

The interface follows the game's language: English, German, French, Spanish, Italian, Brazilian Portuguese, Korean,
Simplified and Traditional Chinese, and Russian. Ukrainian, which the game itself does not offer, is chosen in the
add-on's settings.

## How it works

Add-ons cannot open files or sockets, so the two halves talk through the only doors the game leaves open:

```mermaid
flowchart LR
    subgraph Game
        A[Add-on]
    end
    subgraph Computer
        C[Companion app]
        M[Game data server<br/>MCP over stdio]
        X[AI agent CLI]
    end
    A -- "toggles 128 marker add-ons;<br/>the game saves them to AddOns.txt" --> C
    C -- "writes a Lua payload;<br/>the add-on loads it on demand" --> A
    C <--> X
    X <--> M
```

- **Game → computer.** The add-on encodes each message as 16-byte frames in the enabled/disabled state of 128 marker
  add-ons and asks the game to save the add-on list. The companion watches `AddOns.txt` and decodes the frames. A
  frame lives on disk for 40 ms and the companion reads it from outside, so every 48 data frames are followed by six
  check frames: any six frames missed in a row are rebuilt, and a packet that still does not arrive is sent again.
- **Computer → game.** The companion writes one Lua file. The add-on reads it by loading a fresh load-on-demand
  add-on each time — 512 of them per interface session.
- **Advice.** The companion runs an agent you already have: the [Codex CLI](https://learn.chatgpt.com/docs/codex/cli)
  or [Claude Code](https://code.claude.com/docs/en/setup). You choose one in the companion's menu and everything
  goes to it — chats and journal research alike. Either way the agent is confined to the adviser's role: it reads
  character data through a local MCP server, searches the web and reads pages, and can do nothing else. Nothing but
  the question and the tool results it asks for leaves the computer — apart from the auction download below, when
  you set it up.
- **Journal research** runs as hidden one-shot jobs with a fixed output schema. The companion validates every answer
  in Go — quotes must appear in the fetched text, IDs must be ones the game sent, links must be `https` — and caches
  it on disk.

The adviser's standing instructions are in [helper/game-adviser.md](helper/game-adviser.md).

## Cost and privacy

- Advice uses **your own** subscription: a Claude plan through Claude Code, or a ChatGPT plan through the Codex CLI.
  One of them is enough. There is no server of ours and no account to create.
- What is sent to the model: your question, and the sections of the character snapshot the agent requests. You
  choose the allowed sections in *Settings → Character data*.
- Journal research spends the same allowance, so by default it runs **only when you press a button** in a section.
  *Settings → Advisor* has the switch to *Automatic*: then the hotfix digest and class advice are prepared after
  login and the other sections when you open them. The same page sets how many research jobs may run in a day
  (60 unless you change it, 200 at most). Answers are cached on disk, so asking again for the same thing is free
  until the answer goes stale.
- The chat footer's tooltip shows which adviser answers and how much of its allowance is left, when the provider
  reports that: Codex through its account limits, Claude Code after each turn.
- Conversations, snapshots, memory and cached research stay on your disk.
- Auction prices, if you set them up, come from Blizzard's Game Data API with your own API client. The companion
  asks for nothing but your market's auction snapshot (region, ruleset and faction pick it); no character data is
  sent. For upgrade searches it also downloads the game's item table (item class and equipment slot) from
  [wago.tools](https://wago.tools) once a day. Prices, their history and the table stay on your disk.

## Setup

**You need:** macOS 12 or later, the game client with a `_forever_` folder, and **one** of the two agents: Claude
Code with a Claude plan, or the Codex CLI with a ChatGPT plan.

1. Download [looking-for-guidance-macOS.zip](https://github.com/illia-chaika/looking-for-guidance/releases/latest/download/looking-for-guidance-macOS.zip)
   and open the app inside. It offers to move itself to the Applications folder.
2. Pick the game folder from its menu bar icon and let it install the add-on.
3. In the game, type `/ask`.

The app looks for a new version once a day, or when you choose *Check for Updates…* in its menu, and installs it
while the game is closed. Versions 0.10.0 and 0.10.1 never check: download the current one once by hand. To build it
yourself, see [Development](#development).

**On Windows (experimental).** The Windows download is built from the same code but has never been run on Windows,
so expect problems, and please report what you find in an
[issue](https://github.com/illia-chaika/looking-for-guidance/issues). It needs Windows 10 or later on x64.

1. Download [looking-for-guidance-Windows.zip](https://github.com/illia-chaika/looking-for-guidance/releases/latest/download/looking-for-guidance-Windows.zip),
   unzip it and open `Looking for Guidance.exe`. It moves itself to `%LOCALAPPDATA%\Programs\looking-for-guidance`
   and adds itself to the Start menu.
2. The app is not signed, so Microsoft Defender SmartScreen stops it with *Windows protected your PC*. Choose
   **More info**, then **Run anyway**.
3. Pick the game folder from its icon in the notification area, let it install the add-on, and type `/ask` in the
   game.

The adviser is chosen in the menu under *Adviser*, and everything goes to the one you chose. If you have only
Claude Code installed, the companion uses it without asking; nothing of Codex is started or created.

**With Claude Code** there is nothing else to set up: sign in once. While the game runs, the menu offers what is
missing. *Install Claude Code…* opens Terminal (PowerShell on Windows) with the official installer
(`curl -fsSL https://claude.ai/install.sh | bash`) followed by `claude auth login`, which finishes in the browser;
*Sign in to Claude Code…* runs only the sign-in. The companion starts Claude Code without your own Claude settings,
memory files, skills or MCP servers, so the adviser behaves the same on every machine.

**With Codex**, the companion keeps the adviser apart from your own Codex setup in a profile it creates itself on
first use (`~/Library/Application Support/Looking for Guidance Codex/config.toml`: read-only sandbox, no shell, live
web search, the game data server). That profile has its own sign-in: when the menu says *Codex is not signed in to
ChatGPT*, choose *Sign in to ChatGPT…* and finish in the browser. An existing profile is never rewritten, so you can
change its default model there.

### Settings

The menu covers the everyday choices. The rest lives in `~/Library/Application Support/Looking for Guidance/settings.json`:

| Key | Meaning |
| --- | --- |
| `wowDir` | The game folder, ending in `_forever_` |
| `runWithWoW` | Start and quit the companion together with the game |
| `provider` | `"claude"` for Claude Code; absent for Codex. Set by the *Adviser* menu, and by the first start when only Claude Code is installed |
| `codexPath` | Full path of the Codex CLI when it is not on `PATH` |
| `language` | Language of the companion's menu, such as `"ru"` or `"de"`; by default it follows the system |
| `gameDir` | The agent's working folder; chats are tied to it, so it only matters when moving an existing setup |
| `hotfixUrl` | Address of Blizzard's hotfix article, if it moves |

### Auction prices

Blizzard serves WoW: Forever's auction houses through its Game Data API, about once an hour. To read them the
companion needs your own API client, which is free. Setting it up is the same on macOS and Windows (where, like the
rest of the Windows build, it has not been tried yet):

1. Choose *Auction prices* in the companion's menu (its menu bar icon on macOS, its icon in the notification area on
   Windows). Your browser opens <https://develop.battle.net/access/clients>: sign in with your Battle.net account and
   create a client there.
2. The companion asks for the client's ID, then for its secret, which is typed as dots. It asks Blizzard whether the
   two work and stores them only if they do.
3. Ask anything in the game. The add-on reports your region, ruleset and faction with each question, and the
   companion downloads that auction house within a few minutes.

A check mark next to *Auction prices* means a client is stored. The ID and secret are kept in the system's credential
store, never in a file:

- macOS: the login keychain, items `lfg-battlenet-id` and `lfg-battlenet-secret` with the account `battlenet`. Items
  made with the `security` commands that earlier versions asked for keep working.
- Windows: Credential Manager, under Windows Credentials, as the generic credentials `lfg-battlenet-id:battlenet` and
  `lfg-battlenet-secret:battlenet`.

While it runs, the companion checks every market the game reported in the past week every 20 minutes and downloads
it when Blizzard has a newer snapshot. It keeps a summary in the `auctions` folder beside `settings.json`
(`~/Library/Application Support/Looking for Guidance/auctions/` on macOS, `%AppData%\Looking for Guidance\auctions\`
on Windows) and a price history for 90 days (about 1 MB a day per market, compressed), and offers the adviser two
tools: `get_auction_prices` for the price of named items, and `find_auction_upgrades`, which picks the auctions for one
equipment slot within a budget and lets the running game drop what the character cannot wear and read each
candidate's exact stats. For that search the companion also keeps the game's item table from wago.tools, checked once
a day.

To see that it works, look for `Auction house EU Normal Horde (house 114): new snapshot downloaded`, with your own
market, in `helper.log` beside `settings.json`; the journal's Bags section then shows prices with the snapshot's time.

Without prices nothing breaks. The adviser always has both tools, and they answer with the reason and what to do,
which the adviser passes on in your language; the Bags section says it in one line. The reasons are:

- No client is set up: choose *Auction prices*.
- Blizzard no longer accepts the stored client, as after deleting it or resetting its secret: choose *Auction prices*
  twice, to turn it off and to set it up again with a working client.
- The character's market has no data in the API. Normal and PvP markets are known; which auction house serves each
  Roleplay ruleset is not known yet, and hardcore characters trade on a market the API does not list.
- The first download is still running, or failing; the companion tries again every 20 minutes and `helper.log` says
  why.

To switch it off, choose *Auction prices* again and confirm: the companion removes the client from the credential
store and stops downloading. Prices already downloaded stay on your disk.

## Known issues

- The companion can miss frames while the game is busy. Up to six in a row are rebuilt from check frames and a
  packet that is still incomplete is sent once more, so the worst case is a delay; `helper.log` records every miss.
- Claude Code as the adviser has had less play than Codex. In the game it has answered chat questions with the
  character's data and prepared the hotfix digest; the other kinds of journal research were checked outside the
  game only.
- Development and play have been on Apple Silicon with a current macOS. Intel Macs and macOS 12 to 15 are built for
  but have not been tried.

Found something else, or have an idea? Open an [issue](https://github.com/illia-chaika/looking-for-guidance/issues/new/choose).

## Development

| Path | Contents |
| --- | --- |
| `looking-for-guidance/` | The Lua add-on: interface, journal cards, translations, fonts and artwork |
| `helper/` | The companion in Go: transport, agent sessions, MCP server, journal research, tray |
| `tools/` | Build and local install scripts, emoji atlas and journal artwork generators |
| `test/`, `samples/` | Lua test harness that stands in for the game client, installer checks, sample payloads |
| `docs/` | The pictures in this README, drawn from the add-on's colours, fonts and generated artwork |

Building needs Xcode command line tools, Go (the toolchain named in `go.mod`) and Python 3; the tests also need Lua.

```sh
./tools/build-macos.sh                     # builds dist/Looking for Guidance.app for Apple Silicon and Intel, ad-hoc signed
python3 tools/install-local-macos.py       # installs the app and the add-on; close the game first
```

Maintainers make the release download with `./tools/release-macos.sh`: it signs with Developer ID, has Apple notarize
the app and packs `dist/looking-for-guidance-macOS.zip`. It needs a Developer ID identity in the keychain
(`LFG_SIGN_IDENTITY` picks one if there are several) and a profile from `xcrun notarytool store-credentials`
(`LFG_NOTARY_PROFILE`, default `lfg-notary`).

`./tools/build-windows.sh` makes the experimental Windows download on a Mac: `dist/looking-for-guidance-Windows.zip`,
unsigned, with the executable (which carries the add-on) and the licences. On Windows, with that ZIP in `dist/`,
`go test -run TestBuiltZipUnpacks ./helper` unpacks it with the updater's own code.

```sh
go test ./...
(for t in test/*.lua; do [ "$t" = test/wow.lua ] || lua "$t" || exit 1; done)
python3 test/install_quiesce.py && python3 test/install_addon_payload.py
```

Three checks talk to the real world and are off by default:

- `FOREVER_LIVE=digest go test -run TestLiveInsight -v ./helper` runs one journal research job against the real
  agent and the real hotfix page (`quest`, `brief`, `dungeon`, `bags` and `leveling` work too). It spends allowance.
  Add `FOREVER_LIVE_AGENT=claude` to run it through Claude Code, and `FOREVER_MARKET="EU PvE Horde"` to give the
  `bags` job a real auction house first (needs the Battle.net client stored through *Auction prices*).
- `FOREVER_AUCTION="EU PvE Horde" go test -run TestLiveAuction -v ./helper` downloads that auction house with the
  Battle.net client stored through *Auction prices* and checks the summary. The other tests keep their client in
  memory and never touch the credential store.
- `FOREVER_FRAMES=/Volumes/GameDisk go test -run TestFrameSoak -v ./helper` writes `AddOns.txt` to a temporary
  folder on that disk the way the game does and reads it with the companion's own watcher.

Journal artwork is drawn by `tools/build-journal-art.py` from nothing but numbers; icons are the game's own,
referenced by path.

## Roadmap

- A tested and signed Windows build: the experimental one has not run on Windows yet

## Licence and credits

Looking for Guidance is free software under the [GNU General Public License v3.0](LICENSE). It is a modified version
of [thott.bot chat 0.4.5](https://github.com/thott-bot/chat/releases/tag/v0.4.5), Copyright © 2026 Eden Blue
Ventures LLC; [NOTICE.md](NOTICE.md) lists what changed and the licences of bundled fonts and emoji.

World of Warcraft and Blizzard Entertainment are trademarks or registered trademarks of Blizzard Entertainment, Inc.
This project is not affiliated with or endorsed by Blizzard Entertainment.
