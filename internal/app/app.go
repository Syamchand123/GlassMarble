package app

import (
	"path/filepath"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/config"
	"github.com/Syamchand123/GlassMarble/internal/logger"
)

type App struct {
	Config *config.Config
	Logger *logger.Logger
	AKG    *akg.AKGTransactionManager
}

func New(flagConfig config.Config) (*App, error) {
	// 1. Load config
	cfg, err := config.Load(flagConfig)
	if err != nil {
		return nil, err
	}

	// 2. Init logger
	log := logger.New(cfg.Debug)

	// 3. Init AKG (if available)
	akgPath := filepath.Join(cfg.RootDir, cfg.StorageDir)
	tm, _ := akg.NewAKGTransactionManager(akgPath)

	return &App{
		Config: cfg,
		Logger: log,
		AKG:    tm,
	}, nil
}
