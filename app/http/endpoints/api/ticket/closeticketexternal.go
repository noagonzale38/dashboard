package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/TicketsBot-cloud/common/closerelay"
	"github.com/TicketsBot-cloud/dashboard/app"
	"github.com/TicketsBot-cloud/dashboard/database"
	"github.com/TicketsBot-cloud/dashboard/redis"
	"github.com/TicketsBot-cloud/dashboard/rpc/cache"
	"github.com/TicketsBot-cloud/dashboard/utils"
	cache2 "github.com/TicketsBot-cloud/gdl/cache"
	"github.com/gin-gonic/gin"
)

// CloseTicketExternal closes a ticket in response to an API-key-authenticated
// request. It mirrors CloseTicket but skips the per-user permission check on
// the dashboard side (authorization is the API key itself). The close payload
// is attributed to the guild owner so the worker's downstream CanClose
// permission check always passes.
func CloseTicketExternal(c *gin.Context) {
	guildId, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, utils.ErrorStr("Invalid guild ID provided: %s", c.Param("id")))
		return
	}

	ticketId, err := strconv.Atoi(c.Param("ticketId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, utils.ErrorStr("Invalid ticket ID provided: %s", c.Param("ticketId")))
		return
	}

	var body closeBody
	// Body is optional for the external endpoint; ignore bind errors so callers
	// can send an empty body.
	_ = c.ShouldBindJSON(&body)

	ticket, err := database.Client.Tickets.Get(c, ticketId, guildId)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, app.NewError(err, "Unable to load ticket. Please try again."))
		return
	}

	if ticket.UserId == 0 {
		c.JSON(http.StatusNotFound, utils.ErrorStr("Ticket #%d not found", ticketId))
		return
	}

	// The worker re-checks permissions on the actor. Use the guild owner so
	// the check is always satisfied, regardless of which guild the call targets.
	ownerId, err := cache.Instance.GetGuildOwner(c, guildId)
	if err != nil {
		if errors.Is(err, cache2.ErrNotFound) {
			c.JSON(http.StatusNotFound, utils.ErrorStr("Guild not found"))
			return
		}
		_ = c.AbortWithError(http.StatusInternalServerError, app.NewError(err, "Failed to fetch guild owner"))
		return
	}

	data := closerelay.TicketClose{
		GuildId:  guildId,
		TicketId: ticket.Id,
		UserId:   ownerId,
		Reason:   body.Reason,
	}

	if err := closerelay.Publish(redis.Client.Client, data); err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, app.NewError(err, "Failed to publish ticket close event to Redis"))
		return
	}

	c.JSON(200, utils.SuccessResponse)
}
