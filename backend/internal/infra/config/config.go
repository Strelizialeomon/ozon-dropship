// Package config 加载并校验配置，提供全局单例。
//
// 结构照 xhs-analysis：顶层 server 节全局共享（port、mode、trusted_proxies），
// mode(release|debug) 决定加载哪个 database 分节；其余组件各有可选分节，缺省即静默不启用
// 或落到默认值（标准档：Init 对可选段不报错）。调用顺序：先 Init(path)，后 Get()。
package config

import (
	"fmt"
	"sync"
	"time"

	"github.com/spf13/viper"
)

// ServerConfig HTTP 服务配置。
type ServerConfig struct {
	Port int    `mapstructure:"port"`
	Mode string `mapstructure:"mode"` // debug | release
	// TrustedProxies 信任的反代 IP（如同机 Caddy 填 127.0.0.1）。
	// 留空 = 不信任任何代理头，伪造的 X-Forwarded-For 一律无效。
	TrustedProxies []string `mapstructure:"trusted_proxies"`
}

// DatabaseConfig MySQL 连接配置。
type DatabaseConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	Database string `mapstructure:"database"`
}

// DSN 生成 MySQL 连接串。
// loc=UTC：驱动侧把时间列按 UTC 解释（总纲 §6：时间一律按 UTC 存）；
// time_zone='+00:00'：把会话时区也钉成 UTC——否则 `DEFAULT CURRENT_TIMESTAMP(3)`
// 这类由 MySQL 侧生成的默认值会按服务器本地时区写，非 UTC 服务器上会整体偏时差。
func (c *DatabaseConfig) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=UTC&time_zone=%%27%%2B00%%3A00%%27",
		c.Username, c.Password, c.Host, c.Port, c.Database)
}

// RedisConfig Redis 连接配置（任务队列 + 登录会话共用）。
type RedisConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

// VaultConfig 凭据保险箱配置（总纲 §5.4）。
type VaultConfig struct {
	// CredentialsDir systemd LoadCredentialEncrypted 注入的凭据目录（$CREDENTIALS_DIRECTORY）。
	CredentialsDir string `mapstructure:"credentials_dir"`
	// MasterKeyFile 目录下存放 Tink keyset JSON 的文件名。
	MasterKeyFile string `mapstructure:"master_key_file"`
}

// SessionConfig 登录会话配置（scs + Redis）。
type SessionConfig struct {
	CookieName    string `mapstructure:"cookie_name"`
	LifetimeHours int    `mapstructure:"lifetime_hours"`
}

// Lifetime 会话有效期。
func (c *SessionConfig) Lifetime() time.Duration {
	h := c.LifetimeHours
	if h <= 0 {
		h = DefaultSessionLifetimeHours
	}
	return time.Duration(h) * time.Hour
}

// FeishuConfig 飞书机器人通知配置。
type FeishuConfig struct {
	WebhookURL string `mapstructure:"webhook_url"`
	Secret     string `mapstructure:"secret"`
	// DedupeWindowMinutes 同一错误去重窗口（默认 60 分钟）。
	DedupeWindowMinutes int `mapstructure:"dedupe_window_minutes"`
	// FlushSeconds 合并发送周期（默认 60 秒：按分钟合并）。
	FlushSeconds int `mapstructure:"flush_seconds"`
}

// SubjectRateLimit 一个限流主体（Ozon 每店 / 1688 每应用）的限额。
type SubjectRateLimit struct {
	// DefaultRPS 总闸：每秒请求数。
	DefaultRPS float64 `mapstructure:"default_rps"`
	// Endpoints 单接口限额（键 = 接口名，值 = 每秒请求数）。
	Endpoints map[string]float64 `mapstructure:"endpoints"`
}

// RateLimitConfig 限流配置（总纲 §5.5）。
type RateLimitConfig struct {
	Ozon    SubjectRateLimit `mapstructure:"ozon"`
	Alibaba SubjectRateLimit `mapstructure:"alibaba"`
}

// QueueConfig asynq 与补投扫描配置。
type QueueConfig struct {
	Concurrency int `mapstructure:"concurrency"`
	// RescanSeconds 补投扫描周期（默认 300 秒，总纲 §13.1）。
	RescanSeconds int `mapstructure:"rescan_seconds"`
	// RetryMaxRetries 失败重试上限（默认 3 次，总纲 §5.5）。
	RetryMaxRetries int `mapstructure:"retry_max_retries"`
	// RetryBackoffSeconds 指数退避基数（秒）。
	RetryBackoffSeconds int `mapstructure:"retry_backoff_seconds"`
}

// AppConfig 全局配置。
type AppConfig struct {
	Server    ServerConfig
	Database  DatabaseConfig
	Redis     RedisConfig
	Vault     VaultConfig
	Session   SessionConfig
	Notify    FeishuConfig
	RateLimit RateLimitConfig
	Queue     QueueConfig
}

// 默认值。
const (
	DefaultSessionLifetimeHours  = 168 // 7 天
	DefaultRescanSeconds         = 300 // 补投扫描 5 分钟
	DefaultQueueConcurrency      = 10
	DefaultRetryMaxRetries       = 3
	DefaultRetryBackoffSeconds   = 1
	DefaultDedupeWindowMinutes   = 60
	DefaultNotifyFlushSeconds    = 60
	DefaultOzonDefaultRPS        = 50.0 // 每 Client-Id 50 次/秒（总纲 §3.1）
	DefaultAlibabaDefaultRPS     = 5.0  // 官方未公开【未验】，实施期实测（总纲 §3.2）
	DefaultSessionCookieName     = "ozon_session"
	DefaultVaultMasterKeyFile    = "vault_keyset.json"
	DefaultServerMode            = "release"
	DefaultServerPort            = 8080
)

var (
	globalConfig *AppConfig
	once         sync.Once
)

// Init 加载配置。必填项缺失返回 error，让 main.go 决定是否退出。
// 同一进程内只生效一次（sync.Once），重复调用返回首次结果。
func Init(configPath string) error {
	var initErr error
	once.Do(func() {
		v := viper.New()
		v.SetConfigType("yaml")
		v.SetConfigName("config")
		if configPath != "" {
			v.AddConfigPath(configPath)
		}
		v.AddConfigPath(".")
		v.AddConfigPath("./config")

		if err := v.ReadInConfig(); err != nil {
			initErr = fmt.Errorf("读取配置文件失败: %w", err)
			return
		}

		// server：全局共享。
		if v.Sub("server") == nil {
			initErr = fmt.Errorf("配置文件中未找到 [server] 分节")
			return
		}
		var serverCfg ServerConfig
		if err := v.Sub("server").Unmarshal(&serverCfg); err != nil {
			initErr = fmt.Errorf("解析 server 配置失败: %w", err)
			return
		}
		if serverCfg.Port == 0 {
			serverCfg.Port = DefaultServerPort
		}
		if serverCfg.Mode == "" {
			serverCfg.Mode = DefaultServerMode
		}

		// database：来自 mode 对应分节。
		sub := v.Sub(serverCfg.Mode)
		if sub == nil {
			initErr = fmt.Errorf("配置文件中未找到 [%s] 分节", serverCfg.Mode)
			return
		}
		var dbCfg DatabaseConfig
		if err := sub.UnmarshalKey("database", &dbCfg); err != nil {
			initErr = fmt.Errorf("解析 database 配置失败: %w", err)
			return
		}
		if dbCfg.Host == "" || dbCfg.Database == "" {
			initErr = fmt.Errorf("config: [%s.database] 的 host/database 必填", serverCfg.Mode)
			return
		}

		// redis：必填（队列与会话都靠它）。
		var redisCfg RedisConfig
		if rs := v.Sub("redis"); rs != nil {
			if err := rs.Unmarshal(&redisCfg); err != nil {
				initErr = fmt.Errorf("解析 redis 配置失败: %w", err)
				return
			}
		}
		if redisCfg.Addr == "" {
			initErr = fmt.Errorf("config: redis.addr 必填")
			return
		}

		// vault：可选；没配 = 保险箱不启用（凭据写入会被拒绝，绝不落明文）。
		var vaultCfg VaultConfig
		if vs := v.Sub("vault"); vs != nil {
			if err := vs.Unmarshal(&vaultCfg); err != nil {
				initErr = fmt.Errorf("解析 vault 配置失败: %w", err)
				return
			}
		}
		if vaultCfg.MasterKeyFile == "" {
			vaultCfg.MasterKeyFile = DefaultVaultMasterKeyFile
		}

		// session：可选，全有默认值。
		sessionCfg := SessionConfig{CookieName: DefaultSessionCookieName, LifetimeHours: DefaultSessionLifetimeHours}
		if ss := v.Sub("session"); ss != nil {
			if err := ss.Unmarshal(&sessionCfg); err != nil {
				initErr = fmt.Errorf("解析 session 配置失败: %w", err)
				return
			}
			if sessionCfg.CookieName == "" {
				sessionCfg.CookieName = DefaultSessionCookieName
			}
		}

		// notify：可选；webhook_url 为空 = 不启用。
		notifyCfg := FeishuConfig{DedupeWindowMinutes: DefaultDedupeWindowMinutes, FlushSeconds: DefaultNotifyFlushSeconds}
		if ns := v.Sub("notify"); ns != nil {
			if err := ns.UnmarshalKey("feishu", &notifyCfg); err != nil {
				initErr = fmt.Errorf("解析 notify.feishu 配置失败: %w", err)
				return
			}
		}
		if notifyCfg.DedupeWindowMinutes <= 0 {
			notifyCfg.DedupeWindowMinutes = DefaultDedupeWindowMinutes
		}
		if notifyCfg.FlushSeconds <= 0 {
			notifyCfg.FlushSeconds = DefaultNotifyFlushSeconds
		}

		// ratelimit：可选，落到总纲默认值。
		rlCfg := RateLimitConfig{
			Ozon:    SubjectRateLimit{DefaultRPS: DefaultOzonDefaultRPS},
			Alibaba: SubjectRateLimit{DefaultRPS: DefaultAlibabaDefaultRPS},
		}
		if rlSub := v.Sub("ratelimit"); rlSub != nil {
			if err := rlSub.Unmarshal(&rlCfg); err != nil {
				initErr = fmt.Errorf("解析 ratelimit 配置失败: %w", err)
				return
			}
			if rlCfg.Ozon.DefaultRPS <= 0 {
				rlCfg.Ozon.DefaultRPS = DefaultOzonDefaultRPS
			}
			if rlCfg.Alibaba.DefaultRPS <= 0 {
				rlCfg.Alibaba.DefaultRPS = DefaultAlibabaDefaultRPS
			}
		}

		// queue：可选，全有默认值。
		queueCfg := QueueConfig{
			Concurrency:         DefaultQueueConcurrency,
			RescanSeconds:       DefaultRescanSeconds,
			RetryMaxRetries:     DefaultRetryMaxRetries,
			RetryBackoffSeconds: DefaultRetryBackoffSeconds,
		}
		if qs := v.Sub("queue"); qs != nil {
			if err := qs.Unmarshal(&queueCfg); err != nil {
				initErr = fmt.Errorf("解析 queue 配置失败: %w", err)
				return
			}
			if queueCfg.Concurrency <= 0 {
				queueCfg.Concurrency = DefaultQueueConcurrency
			}
			if queueCfg.RescanSeconds <= 0 {
				queueCfg.RescanSeconds = DefaultRescanSeconds
			}
			if queueCfg.RetryMaxRetries < 0 {
				queueCfg.RetryMaxRetries = DefaultRetryMaxRetries
			}
			if queueCfg.RetryBackoffSeconds <= 0 {
				queueCfg.RetryBackoffSeconds = DefaultRetryBackoffSeconds
			}
		}

		globalConfig = &AppConfig{
			Server:    serverCfg,
			Database:  dbCfg,
			Redis:     redisCfg,
			Vault:     vaultCfg,
			Session:   sessionCfg,
			Notify:    notifyCfg,
			RateLimit: rlCfg,
			Queue:     queueCfg,
		}
	})
	return initErr
}

// Get 获取全局配置单例；Init 未调用时 panic（启动期装配错误）。
func Get() *AppConfig {
	if globalConfig == nil {
		panic("配置尚未初始化，请先调用 config.Init()")
	}
	return globalConfig
}
