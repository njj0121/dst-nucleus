package main

var (
	AnnounceGameUpdate        = "检测到游戏版本更新"
	AnnounceModUpdate         = "模组更新已就绪"
	AnnounceGraceRebootPrefix = "，服务器已自动保存，请尽快退出游戏，服务器将在 "
	AnnounceGraceRebootSuffix = " 秒后强制关闭..."
)

func InitAnnounceLang() {
	switch GlobalConf.Section1.AnnounceLang {
	case "zh":
	case "zh-t":
		AnnounceGameUpdate = "檢測到遊戲版本更新"
		AnnounceModUpdate = "模組更新已就緒"
		AnnounceGraceRebootPrefix = "，伺服器已自動保存，請盡快退出遊戲，伺服器將在 "
		AnnounceGraceRebootSuffix = " 秒後強制關閉..."
	case "en":
		AnnounceGameUpdate = "Game update detected"
		AnnounceModUpdate = "Mod update ready"
		AnnounceGraceRebootPrefix = ". Server saved. Please disconnect safely. Restarting in "
		AnnounceGraceRebootSuffix = "s..."
	case "jp":
		AnnounceGameUpdate = "ゲームアップデートを検出"
		AnnounceModUpdate = "MODの更新準備が完了"
		AnnounceGraceRebootPrefix = "。サーバーは保存されました。安全な場所でログアウトしてください。再起動まで残り "
		AnnounceGraceRebootSuffix = " 秒..."
	case "ru":
		AnnounceGameUpdate = "Обновление игры обнаружено"
		AnnounceModUpdate = "Обновление мода готово"
		AnnounceGraceRebootPrefix = ". Сервер сохранён. Пожалуйста, безопасно отключитесь. Перезагрузка через "
		AnnounceGraceRebootSuffix = " секунд..."
	default:
	}
}
