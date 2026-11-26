package site

import "time"

type App struct {
	ID            uint            `gorm:"primaryKey"`
	Slug          string          `gorm:"size:180;uniqueIndex;not null"`
	Name          string          `gorm:"size:200;not null"`
	PackageName   string          `gorm:"size:255;not null"`
	Developer     string          `gorm:"size:200"`
	Version       string          `gorm:"size:80"`
	Category      string          `gorm:"size:80"`
	Language      string          `gorm:"size:12;index;not null;default:id"`
	Description   string          `gorm:"type:text"`
	Rating        string          `gorm:"size:20"`
	Downloads     string          `gorm:"size:40"`
	IconPath      string          `gorm:"size:500"`
	SourceURL     string          `gorm:"size:1000"`
	SourceName    string          `gorm:"size:100"`
	CapturedAt    time.Time       `gorm:"not null"`
	TemporaryData bool            `gorm:"not null;default:true"`
	Active        bool            `gorm:"not null;default:true;index"`
	Screenshots   []AppScreenshot `gorm:"foreignKey:AppID;constraint:OnDelete:CASCADE"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type AppScreenshot struct {
	ID        uint   `gorm:"primaryKey"`
	AppID     uint   `gorm:"not null;index;uniqueIndex:idx_app_position"`
	Position  int    `gorm:"not null;uniqueIndex:idx_app_position"`
	LocalPath string `gorm:"size:500;not null"`
	SourceURL string `gorm:"size:1000"`
	CreatedAt time.Time
}

type SiteConfig struct {
	ID                 uint   `gorm:"primaryKey"`
	SiteKey            string `gorm:"size:120;uniqueIndex;not null"`
	DisplayName        string `gorm:"size:200;not null"`
	DefaultLanguage    string `gorm:"size:12;not null;default:id"`
	RoutePrefix        string `gorm:"size:120;not null;default:''"`
	DownloadBaseURL    string `gorm:"size:500"`
	ImageBasePath      string `gorm:"size:255;not null;default:/assets"`
	ExternalLinkLimit  int    `gorm:"not null;default:12"`
	MaxRelatedLinks    int    `gorm:"not null;default:6"`
	ShowListPanel      bool   `gorm:"not null;default:false"`
	ShowSubdomainPanel bool   `gorm:"not null;default:false"`
	ShowExternalPanel  bool   `gorm:"not null;default:false"`
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type DomainBinding struct {
	ID        uint   `gorm:"primaryKey"`
	Domain    string `gorm:"size:255;uniqueIndex;not null"`
	Language  string `gorm:"size:12;not null"`
	SiteKey   string `gorm:"size:120;index;not null"`
	Remark    string `gorm:"size:500"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

type DomainImageBinding struct {
	ID            uint   `gorm:"primaryKey"`
	Domain        string `gorm:"size:255;uniqueIndex;not null"`
	SiteKey       string `gorm:"size:120;index;not null"`
	ImageBasePath string `gorm:"size:255;not null"`
	Remark        string `gorm:"size:500"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type SiteLink struct {
	ID            uint   `gorm:"primaryKey"`
	SiteKey       string `gorm:"size:120;index;not null"`
	AppSlug       string `gorm:"size:180;index"`
	Kind          string `gorm:"size:20;index;not null"`
	Language      string `gorm:"size:12;index;not null"`
	Domain        string `gorm:"size:255;index"`
	AnchorText    string `gorm:"size:255;not null"`
	TargetURL     string `gorm:"size:1000;not null"`
	Category      string `gorm:"size:100"`
	RecommendTier int    `gorm:"not null;default:0;index"`
	Weight        int    `gorm:"not null;default:1"`
	Enabled       bool   `gorm:"not null;default:true;index"`
	Remark        string `gorm:"size:500"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func Migrate(db interface{ AutoMigrate(...any) error }) error {
	return db.AutoMigrate(&App{}, &AppScreenshot{}, &SiteConfig{}, &DomainBinding{}, &DomainImageBinding{}, &SiteLink{})
}
