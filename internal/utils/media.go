package utils

import "strings"

var subtitleExt = map[string]struct{}{"srt": {}, "ass": {}, "ssa": {}, "vtt": {}}
var audioExt = map[string]struct{}{"mp3": {}, "m4a": {}, "aac": {}, "flac": {}, "wav": {}, "ogg": {}, "opus": {}, "wma": {}, "ape": {}}
var videoExt = map[string]struct{}{
	"mp4": {}, "mkv": {}, "avi": {}, "mov": {}, "wmv": {}, "flv": {}, "m4v": {}, "webm": {},
	"m2ts": {}, "mts": {}, "ts": {}, "mpg": {}, "mpeg": {}, "rm": {}, "rmvb": {}, "3gp": {},
}

func Ext(name string) string {
	idx := strings.LastIndexByte(name, '.')
	if idx < 0 || idx == len(name)-1 {
		return ""
	}
	return strings.ToLower(name[idx+1:])
}

func IsFolder(itemType int) bool { return itemType == 1 }

func IsMedia(name string, itemType int) bool {
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".ts") || strings.HasSuffix(lower, ".mpg") {
		return true
	}
	if itemType == 2 || itemType == 3 {
		return true
	}
	_, ok := videoExt[Ext(name)]
	return ok
}

func Ignore(name string, itemType int) bool {
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".ts") || strings.HasSuffix(lower, ".mpg") {
		return false
	}
	if IsMedia(name, itemType) {
		return false
	}
	return itemType == 0 || itemType == 4
}

func IsSubtitle(name string) bool {
	_, ok := subtitleExt[Ext(name)]
	return ok
}

func IsAudio(name string) bool {
	_, ok := audioExt[Ext(name)]
	return ok
}
