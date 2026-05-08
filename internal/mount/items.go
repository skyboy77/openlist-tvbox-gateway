package mount

import (
	"sort"
	"strconv"

	"openlist-tvbox/internal/i18n"
	"openlist-tvbox/internal/storage"
	"openlist-tvbox/internal/utils"
)

func splitItems(items []storage.Item) ([]storage.Item, []storage.Item) {
	folders := []storage.Item{}
	files := []storage.Item{}
	for _, item := range items {
		if utils.Ignore(item.Name, item.Type) {
			continue
		}
		if utils.IsFolder(item.Type) {
			folders = append(folders, item)
		} else {
			files = append(files, item)
		}
	}
	return folders, files
}

func orderedMediaItems(items []storage.Item, selectedName string) []storage.Item {
	selected := []storage.Item{}
	others := []storage.Item{}
	for _, item := range items {
		if !utils.IsMedia(item.Name, item.Type) {
			continue
		}
		if item.Name == selectedName {
			selected = append(selected, item)
			continue
		}
		others = append(others, item)
	}
	sort.SliceStable(others, func(i, j int) bool {
		return mediaNameLess(others[i].Name, others[j].Name)
	})
	return append(selected, others...)
}

func selectedMediaItem(items []storage.Item, selectedName string) (storage.Item, bool) {
	for _, item := range items {
		if item.Name == selectedName && utils.IsMedia(item.Name, item.Type) {
			return item, true
		}
	}
	return storage.Item{}, false
}

func hasMedia(items []storage.Item) bool {
	for _, item := range items {
		if utils.IsMedia(item.Name, item.Type) {
			return true
		}
	}
	return false
}

func formatMediaCount(count int, lang string) string {
	value := strconv.Itoa(count)
	if i18n.NormalizeLanguage(lang) == i18n.English {
		if count == 1 {
			return value + " video"
		}
		return value + " videos"
	}
	return value + " 个视频"
}
