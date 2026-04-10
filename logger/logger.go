package logger

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"
)

// Config는 로거 초기화 옵션입니다.
type Config struct {
	Directory  string // 로그 파일 디렉토리 (기본 ./.logs)
	Debug      bool   // true면 JSON + stdout, false면 JSON만
	Service    string // service 필드값 (예: capstone-worker)
	ServerType string // serverType 필드값 (예: go)
}

var (
	global *slog.Logger
	cfg    Config
)

// Init은 로거를 초기화합니다. 프로그램 시작 시 반드시 호출되어야 합니다.
func Init(c Config) error {
	cfg = c

	if err := os.MkdirAll(c.Directory, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	logFile, err := newRotatingFileWriter(c.Directory, "structured-http.log")
	if err != nil {
		return err
	}
	/*

		// lumberjack으로 파일 rotation (20MB, 14일, 1GB 총량)
		logFile := &lumberjack.Logger{
			Filename:   filepath.Join(c.Directory, "worker.log"),
			MaxSize:    20, // MB
			MaxAge:     14, // days
			MaxBackups: 50, // 파일 수 상한 (총량 제한용)
			LocalTime:  true,
			Compress:   false,
		}

		// JSON 핸들러 (메인서버 포맷에 맞춰 필드 이름 변경)
	*/
	jsonOpts := &slog.HandlerOptions{
		Level:       slog.LevelInfo,
		ReplaceAttr: replaceAttr,
	}
	jsonHandler := slog.NewJSONHandler(logFile, jsonOpts)

	// 기본 속성: service, serverType (모든 로그에 포함)
	baseAttrs := []slog.Attr{
		slog.String("service", c.Service),
		slog.String("serverType", c.ServerType),
	}

	var handler slog.Handler
	if c.Debug {
		textH := newTextHandler(os.Stdout, slog.LevelDebug)
		handler = newMultiHandler(jsonHandler, textH)
	} else {
		handler = jsonHandler
	}
	handler = handler.WithAttrs(baseAttrs)

	global = slog.New(handler)
	slog.SetDefault(global)
	return nil
}

// replaceAttr는 메인서버 포맷에 맞춰 slog 기본 필드명을 변환합니다.
// time → timestamp (ISO8601 + tz), msg → message
func replaceAttr(groups []string, a slog.Attr) slog.Attr {
	switch a.Key {
	case slog.TimeKey:
		if t, ok := a.Value.Any().(time.Time); ok {
			seoul, err := time.LoadLocation("Asia/Seoul")
			if err == nil {
				t = t.In(seoul)
			}
			return slog.String("timestamp", t.Format("2006-01-02T15:04:05.000-07:00"))
		}
	case slog.MessageKey:
		return slog.Attr{Key: "message", Value: a.Value}
	}
	return a
}

// L은 전역 로거를 반환합니다. Init 호출 전에는 stderr 기본 핸들러 로거를 반환합니다.
func L() *slog.Logger {
	if global == nil {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return global
}
