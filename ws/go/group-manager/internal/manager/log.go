package manager

import (
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DailyLog 按本机日期切换日志文件，不引入额外日志框架。
// SQLite 审计是事实来源，文本日志只是方便 tail/搜索的副本。
type DailyLog struct {
	mu         sync.Mutex
	dir, day   string
	file       *os.File
	retainDays int
}

func NewDailyLog(dir string, days int) (*DailyLog, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &DailyLog{dir: dir, retainDays: days}, nil
}
func (l *DailyLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	day := time.Now().Format("2006-01-02")
	if l.day != day {
		if l.file != nil {
			if err := l.file.Close(); err != nil {
				return 0, err
			}
		}
		f, err := os.OpenFile(filepath.Join(l.dir, "group-manager-"+day+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			l.file = nil
			return 0, err
		}
		l.file = f
		l.day = day
		// 仅清理本程序固定命名的普通日志，不触碰其他文件。
		files, _ := filepath.Glob(filepath.Join(l.dir, "group-manager-????-??-??.log"))
		for _, name := range files {
			base := filepath.Base(name)
			d, err := time.Parse("2006-01-02", base[len("group-manager-"):len("group-manager-")+10])
			if err == nil && time.Since(d) > time.Duration(l.retainDays+1)*24*time.Hour {
				_ = os.Remove(name)
			}
		}
	}
	return l.file.Write(p)
}
func (l *DailyLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

var _ io.WriteCloser = (*DailyLog)(nil)
