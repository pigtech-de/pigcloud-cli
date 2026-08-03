package sync

type ActivityFunc func(path, direction string, bytes int64, err error)
