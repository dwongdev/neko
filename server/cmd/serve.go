package cmd

import (
	"os"
	"os/signal"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/m1k1o/neko/server/internal/api"
	"github.com/m1k1o/neko/server/internal/capture"
	"github.com/m1k1o/neko/server/internal/config"
	"github.com/m1k1o/neko/server/internal/desktop"
	"github.com/m1k1o/neko/server/internal/http"
	"github.com/m1k1o/neko/server/internal/member"
	"github.com/m1k1o/neko/server/internal/plugins"
	"github.com/m1k1o/neko/server/internal/session"
	"github.com/m1k1o/neko/server/internal/webrtc"
	"github.com/m1k1o/neko/server/internal/websocket"
)

func init() {
	service := serve{}

	command := &cobra.Command{
		Use:    "serve",
		Short:  "serve neko streaming server",
		Long:   `serve neko streaming server`,
		PreRun: service.PreRun,
		Run:    service.Run,
	}

	if err := service.Init(command); err != nil {
		log.Panic().Err(err).Msg("unable to initialize configuration")
	}

	root.AddCommand(command)
}

type serve struct {
	logger zerolog.Logger

	configs struct {
		Desktop config.Desktop
		Capture config.Capture
		WebRTC  config.WebRTC
		Member  config.Member
		Session config.Session
		Plugins config.Plugins
		Server  config.Server
	}

	managers struct {
		desktop   *desktop.DesktopManagerCtx
		capture   *capture.CaptureManagerCtx
		webRTC    *webrtc.WebRTCManagerCtx
		member    *member.MemberManagerCtx
		session   *session.SessionManagerCtx
		webSocket *websocket.WebSocketManagerCtx
		plugins   *plugins.ManagerCtx
		api       *api.ApiManagerCtx
		http      *http.HttpManagerCtx
	}
}

func (c *serve) Init(cmd *cobra.Command) error {
	if err := c.configs.Desktop.Init(cmd); err != nil {
		return err
	}
	if err := c.configs.Capture.Init(cmd); err != nil {
		return err
	}
	if err := c.configs.WebRTC.Init(cmd); err != nil {
		return err
	}
	if err := c.configs.Member.Init(cmd); err != nil {
		return err
	}
	if err := c.configs.Session.Init(cmd); err != nil {
		return err
	}
	if err := c.configs.Plugins.Init(cmd); err != nil {
		return err
	}
	if err := c.configs.Server.Init(cmd); err != nil {
		return err
	}

	// legacy if explicitly enabled or if unspecified and legacy config is found
	if viper.GetBool("legacy") || !viper.IsSet("legacy") {
		if err := c.configs.Desktop.InitV2(cmd); err != nil {
			return err
		}
		if err := c.configs.Capture.InitV2(cmd); err != nil {
			return err
		}
		if err := c.configs.WebRTC.InitV2(cmd); err != nil {
			return err
		}
		if err := c.configs.Member.InitV2(cmd); err != nil {
			return err
		}
		if err := c.configs.Session.InitV2(cmd); err != nil {
			return err
		}
		if err := c.configs.Server.InitV2(cmd); err != nil {
			return err
		}
	}

	return nil
}

func (c *serve) PreRun(cmd *cobra.Command, args []string) {
	c.logger = log.With().Str("service", "neko").Logger()

	c.configs.Desktop.Set()
	c.configs.Capture.Set()
	c.configs.WebRTC.Set()
	c.configs.Member.Set()
	c.configs.Session.Set()
	c.configs.Plugins.Set()
	c.configs.Server.Set()

	// legacy if explicitly enabled or if unspecified and legacy config is found
	if viper.GetBool("legacy") || !viper.IsSet("legacy") {
		c.configs.Desktop.SetV2()
		c.configs.Capture.SetV2()
		c.configs.WebRTC.SetV2()
		c.configs.Member.SetV2()
		c.configs.Session.SetV2()
		c.configs.Server.SetV2()
	}
}

func (c *serve) Start(cmd *cobra.Command) {
	c.managers.session = session.New(
		&c.configs.Session,
	)

	c.managers.member = member.New(
		c.managers.session,
		&c.configs.Member,
	)

	if err := c.managers.member.Connect(); err != nil {
		c.logger.Panic().Err(err).Msg("unable to connect to member manager")
	}

	c.managers.desktop = desktop.New(
		&c.configs.Desktop,
	)
	c.managers.desktop.Start()

	c.managers.capture = capture.New(
		c.managers.desktop,
		&c.configs.Capture,
	)
	c.managers.capture.Start()

	c.managers.webRTC = webrtc.New(
		c.managers.desktop,
		c.managers.capture,
		&c.configs.WebRTC,
	)
	c.managers.webRTC.Start()

	c.managers.webSocket = websocket.New(
		c.managers.session,
		c.managers.desktop,
		c.managers.capture,
		c.managers.webRTC,
	)
	c.managers.webSocket.Start()

	c.managers.api = api.New(
		c.managers.session,
		c.managers.member,
		c.managers.desktop,
		c.managers.capture,
	)

	c.managers.plugins = plugins.New(
		&c.configs.Plugins,
	)

	// init and set configuration now
	// this means it won't be in --help
	c.managers.plugins.InitConfigs(cmd)
	c.managers.plugins.SetConfigs()

	c.managers.plugins.Start(
		c.managers.session,
		c.managers.webSocket,
		c.managers.api,
	)

	c.managers.http = http.New(
		c.managers.webSocket,
		c.managers.api,
		&c.configs.Server,
	)
	c.managers.http.Start()
}

func (c *serve) Shutdown() {
	var err error

	err = c.managers.http.Shutdown()
	c.logger.Err(err).Msg("http manager shutdown")

	err = c.managers.plugins.Shutdown()
	c.logger.Err(err).Msg("plugins manager shutdown")

	err = c.managers.webSocket.Shutdown()
	c.logger.Err(err).Msg("websocket manager shutdown")

	err = c.managers.webRTC.Shutdown()
	c.logger.Err(err).Msg("webrtc manager shutdown")

	err = c.managers.capture.Shutdown()
	c.logger.Err(err).Msg("capture manager shutdown")

	err = c.managers.desktop.Shutdown()
	c.logger.Err(err).Msg("desktop manager shutdown")

	err = c.managers.member.Disconnect()
	c.logger.Err(err).Msg("member manager disconnect")
}

func (c *serve) Run(cmd *cobra.Command, args []string) {
	c.logger.Info().Msg("starting neko server")
	c.Start(cmd)
	c.logger.Info().Msg("neko ready")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	sig := <-quit

	c.logger.Warn().Msgf("received %s, attempting graceful shutdown", sig)
	c.Shutdown()
	c.logger.Info().Msg("shutdown complete")
}
