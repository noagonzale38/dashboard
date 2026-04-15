package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/TicketsBot-cloud/common/closerelay"
	"github.com/TicketsBot-cloud/dashboard/app"
	"github.com/TicketsBot-cloud/dashboard/config"
	"github.com/TicketsBot-cloud/dashboard/database"
	"github.com/TicketsBot-cloud/dashboard/log"
	"github.com/TicketsBot-cloud/dashboard/redis"
	"github.com/TicketsBot-cloud/dashboard/rpc/cache"
	"github.com/TicketsBot-cloud/dashboard/utils"
	cache2 "github.com/TicketsBot-cloud/gdl/cache"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// CloseTicketExternal closes a ticket in response to an API-key-authenticated
// request. It mirrors CloseTicket but skips the per-user permission check on
// the dashboard side (authorization is the API key itself). The close payload
// is attributed to the guild owner so the worker's downstream CanClose
// permission check always passes. Each step is logged to aid debugging when
// the event is published but the worker fails to act on it.
func CloseTicketExternal(c *gin.Context) {
	logger := log.Logger.With(
		zap.String("handler", "CloseTicketExternal"),
		zap.String("client_ip", c.ClientIP()),
	)
	logger.Info("external close request received",
		zap.String("guild_param", c.Param("id")),
		zap.String("ticket_param", c.Param("ticketId")),
	)

	guildId, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		logger.Warn("invalid guild id", zap.Error(err))
		c.JSON(http.StatusBadRequest, utils.ErrorStr("Invalid guild ID provided: %s", c.Param("id")))
		return
	}

	ticketId, err := strconv.Atoi(c.Param("ticketId"))
	if err != nil {
		logger.Warn("invalid ticket id", zap.Error(err))
		c.JSON(http.StatusBadRequest, utils.ErrorStr("Invalid ticket ID provided: %s", c.Param("ticketId")))
		return
	}

	logger = logger.With(
		zap.Uint64("guild_id", guildId),
		zap.Int("ticket_id", ticketId),
	)

	var body closeBody
	// Body is optional for the external endpoint; ignore bind errors so callers
	// can send an empty body.
	_ = c.ShouldBindJSON(&body)

	ticket, err := database.Client.Tickets.Get(c, ticketId, guildId)
	if err != nil {
		logger.Error("failed to load ticket", zap.Error(err))
		_ = c.AbortWithError(http.StatusInternalServerError, app.NewError(err, "Unable to load ticket. Please try again."))
		return
	}

	if ticket.UserId == 0 {
		logger.Warn("ticket not found")
		c.JSON(http.StatusNotFound, utils.ErrorStr("Ticket #%d not found", ticketId))
		return
	}

	var channelIdLog int64 = -1
	if ticket.ChannelId != nil {
		channelIdLog = int64(*ticket.ChannelId)
	}
	logger.Info("ticket loaded",
		zap.Uint64("opener_id", ticket.UserId),
		zap.Int64("channel_id", channelIdLog),
		zap.Bool("is_open", ticket.Open),
	)

	// The worker re-checks permissions on the actor. Use the guild owner so
	// the check is always satisfied, regardless of which guild the call targets.
	ownerId, err := cache.Instance.GetGuildOwner(c, guildId)
	if err != nil {
		if errors.Is(err, cache2.ErrNotFound) {
			logger.Warn("guild owner not in cache (bot may not be in guild)")
			c.JSON(http.StatusNotFound, utils.ErrorStr("Guild not found"))
			return
		}
		logger.Error("failed to fetch guild owner", zap.Error(err))
		_ = c.AbortWithError(http.StatusInternalServerError, app.NewError(err, "Failed to fetch guild owner"))
		return
	}

	logger.Info("resolved guild owner as close actor", zap.Uint64("owner_id", ownerId))

	data := closerelay.TicketClose{
		GuildId:  guildId,
		TicketId: ticket.Id,
		UserId:   ownerId,
		Reason:   body.Reason,
	}

	marshalled, _ := json.Marshal(data)
	redisAddr := fmt.Sprintf("%s:%d", config.Conf.Redis.Host, config.Conf.Redis.Port)
	logger.Info("publishing close event",
		zap.String("redis_addr", redisAddr),
		zap.String("redis_key", "tickets:close"),
		zap.String("payload", string(marshalled)),
	)

	if err := closerelay.Publish(redis.Client.Client, data); err != nil {
		logger.Error("failed to publish close event to redis", zap.Error(err))
		_ = c.AbortWithError(http.StatusInternalServerError, app.NewError(err, "Failed to publish ticket close event to Redis"))
		return
	}

	// Verify the event is actually sitting in the queue. If the length is 0
	// right after our push, a worker has already consumed it; if it's > 0 and
	// stays > 0, no worker is listening on this Redis instance.
	queueLen, lenErr := redis.Client.Client.LLen(c, "tickets:close").Result()
	if lenErr != nil {
		logger.Warn("failed to read tickets:close queue length after publish", zap.Error(lenErr))
	} else {
		logger.Info("tickets:close queue length after publish", zap.Int64("length", queueLen))
	}

	logger.Info("external close published successfully")
	c.JSON(200, utils.SuccessResponse)
}
