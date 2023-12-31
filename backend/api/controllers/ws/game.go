package ws

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"math/big"
	"time"

	"github.com/jinzhu/copier"

	"github.com/shinyhawk/Quiz/api/middleware"
	"github.com/shinyhawk/Quiz/entity"
	"nhooyr.io/websocket"
)

const (
	Standby    = "GAME_STANDBY"
	Starting   = "GAME_STARTING"
	InProgress = "GAME_IN_PROGRESS"
	Finished   = "GAME_FINISHED"
)

const (
	RoundInProgress = "ROUND_IN_PROGRESS"
	RoundWaiting    = "ROUND_WAITING"
	RoundFinished   = "ROUND_FINISHED"
	AnswerAccepted  = "ANSWER_ACCEPTED"
)

const (
	GameNotFound  = "GAME_NOT_FOUND"
	AlreadyInGame = "ALREADY_IN_GAME"
	NotInGame     = "NOT_IN_GAME"
	NotOwner      = "NOT_OWNER"

	JoinedGame   = "JOINED_GAME"
	LeftGame     = "LEFT_GAME"
	GameDeleted  = "GAME_DELETED"
	UserLeft     = "USER_LEFT"
	UserJoined   = "USER_JOINED"
	UserAnswered = "USER_ANSWERED"
)

type User struct {
	Id             uint            `json:"id"`
	Name           string          `json:"name"`
	ProfilePicture string          `json:"profilePicture"`
	ActiveGame     *Game           `json:"-"`
	Conn           *websocket.Conn `json:"-"`
}

type Game struct {
	Id            uint             `json:"id"`
	InstId        int              `json:"-"`
	Status        string           `json:"status"`
	RoundStatus   string           `json:"roundStatus"`
	CurrentRound  int              `json:"currentRound"`
	Points        float64          `json:"points"`
	Topic         string           `json:"topic"`
	RoundTime     int              `json:"roundTime"`
	QuestionCount int              `json:"questionCount"`
	InviteCode    string           `json:"inviteCode"`
	Members       map[uint]*User   `json:"members"`
	Owner         *User            `json:"owner"`
	Leaderboard   map[uint]float64 `json:"leaderboard"`
	Data          *entity.Game     `json:"-"`
	Rounds        map[int]*Round   `json:"-"`
}

type Round struct {
	Answers map[uint]uint
}

type GameSocketController struct {
	Users    map[uint]*User
	Games    map[string]*Game
	Game     IGameService
	User     IUserService
	Session  ISessionService
	GameTime int
}

type IGameService interface {
	CreateGame(body entity.CreateGame, ownerId uint) (*entity.Game, error)
	UpdateGame(body entity.UpdateGame, id int, code string, ownerId uint) (*entity.Game, error)
	DeleteGame(id int, code string, userId uint) error

	GetGame(id int, code string) (*entity.Game, error)
	GetGamesByOwner(id int, user int, limit int) (*[]entity.Game, error)
	GetFavoriteGames(user int) (*[]entity.Game, error)

	Favorite(id int, userId int) bool
}

type ISessionService interface {
	NewSession(id, userId, instId int) uint
	EndSession(id, userId, instId, questions, players int, points float64) uint
}

type IUserService interface {
	CreateUser(body *entity.CreateUser) (*entity.User, error)
	UpdateUser(id uint, body entity.UpdateUser) (*entity.User, error)
	DeleteUser(id uint)
	GetUser(id uint) *entity.User
	GetUserByProvider(id string, provider string) *entity.User
}

func NewGameSocketController(game IGameService, user IUserService, session ISessionService) *GameSocketController {
	c := new(GameSocketController)

	c.Users = make(map[uint]*User)
	c.Games = make(map[string]*Game)
	c.Game = game
	c.User = user
	c.Session = session
	c.GameTime = 10

	return c
}

func (g *GameSocketController) InitUser(ctx context.Context) (*User, error) {
	authedUser := ctx.Value("authedUser").(middleware.AuthedUser)

	user := g.User.GetUser(authedUser.Id)
	if user == nil {
		return nil, errors.New("no user found")
	}

	_, found := g.Users[user.Id]
	if found {
		return nil, errors.New("user already exists on another socket")
	}

	g.Users[user.Id] = &User{
		Id:             user.Id,
		Name:           user.Name,
		ProfilePicture: user.Picture,
		Conn:           ctx.Value("conn").(*websocket.Conn),
	}

	return g.Users[user.Id], nil
}

func (g *GameSocketController) CleanUser(ctx context.Context) {
	user := ctx.Value("user").(*User)
	if user.ActiveGame != nil {
		g.LeaveGame(ctx)
	}

	delete(g.Users, user.Id)
}

type JoinGameData struct {
	GameId string `json:"gameId"`
}

func (g *GameSocketController) JoinGame(ctx context.Context, msgData json.RawMessage) {
	conn := ctx.Value("conn").(*websocket.Conn)

	var data JoinGameData
	err := json.Unmarshal(msgData, &data)
	if err != nil {
		DataReply(true, "DATA_ERROR", err.Error()).Send(conn)
		return
	}

	user := ctx.Value("user").(*User)
	if user.ActiveGame != nil {
		MessageReply(true, AlreadyInGame).Send(conn)
		return
	}

	// var game entity.Game
	// g.DB.Preload("Questions.Options").Where("invite_code = ?", data.GameId).First(&game)
	game, err := g.Game.GetGame(0, data.GameId)
	if err != nil {
		DataReply(true, "DATA_ERROR", err.Error()).Send(conn)
		return
	}

	if game.Id == 0 {
		MessageReply(true, GameNotFound).Send(conn)
		return
	}

	value, ok := g.Games[game.InviteCode]
	if ok {
		for _, member := range value.Members {
			DataReply(false, UserJoined, user).Send(member.Conn)
		}

		value.Members[user.Id] = user
		user.ActiveGame = value
		value.Leaderboard[user.Id] = 0

		DataReply(false, JoinedGame, value).Send(conn)
		return
	}

	if game.Owner != user.Id {
		MessageReply(true, NotOwner).Send(conn)
		return
	}

	instId, _ := rand.Int(rand.Reader, big.NewInt(100000000000))

	newGame := Game{
		Id:            game.Id,
		InstId:        int(instId.Int64()),
		Status:        Standby,
		RoundStatus:   RoundWaiting,
		Points:        game.Points,
		Topic:         game.Topic,
		QuestionCount: len(game.Questions),
		RoundTime:     game.RoundTime,
		InviteCode:    game.InviteCode,
		Members:       map[uint]*User{},
		Leaderboard:   map[uint]float64{},
		Rounds:        map[int]*Round{},
		Owner:         user,
		Data:          game,
	}

	newGame.Members[user.Id] = user
	newGame.Leaderboard[user.Id] = 0

	g.Games[newGame.InviteCode] = &newGame
	user.ActiveGame = g.Games[newGame.InviteCode]
	DataReply(false, JoinedGame, newGame).Send(conn)
}

type ChatData struct {
	Message string `json:"message"`
}

type ChatBroadcast struct {
	Name    string `json:"name"`
	UserId  uint   `json:"userId"`
	Message string `json:"message"`
}

func (g *GameSocketController) SendChat(ctx context.Context, msgData json.RawMessage) {
	conn := ctx.Value("conn").(*websocket.Conn)
	user := ctx.Value("user").(*User)
	if user.ActiveGame == nil {
		MessageReply(true, NotInGame).Send(conn)
		return
	}

	var data ChatData
	err := json.Unmarshal(msgData, &data)
	if err != nil {
		DataReply(true, "DATA_ERROR", err.Error()).Send(conn)
		return
	}

	game := user.ActiveGame
	for _, member := range game.Members {
		DataReply(false, ReceiveChat, ChatBroadcast{Name: user.Name, Message: data.Message, UserId: user.Id}).Send(member.Conn)
	}
}

func (g *GameSocketController) LeaveGame(ctx context.Context) {
	conn := ctx.Value("conn").(*websocket.Conn)
	user := ctx.Value("user").(*User)
	if user.ActiveGame == nil {
		MessageReply(true, NotInGame).Send(conn)
		return
	}

	game := user.ActiveGame
	if game.Owner == user {
		for _, member := range game.Members {
			member.ActiveGame = nil
			MessageReply(false, GameDeleted).Send(member.Conn)
		}
		delete(g.Games, game.InviteCode)
	}

	delete(game.Members, user.Id)
	user.ActiveGame = nil
	MessageReply(false, LeftGame).Send(conn)

	for _, member := range game.Members {
		DataReply(false, UserLeft, user).Send(member.Conn)
	}
}

func (g *GameSocketController) GetGame(ctx context.Context) {
	conn := ctx.Value("conn").(*websocket.Conn)

	user := ctx.Value("user").(*User)
	if user.ActiveGame == nil {
		MessageReply(true, NotInGame).Send(conn)
		return
	}

	DataReply(false, GetGame, user.ActiveGame).Send(conn)
}

func (g *GameSocketController) IsOwner(ctx context.Context) {
	conn := ctx.Value("conn").(*websocket.Conn)

	user := ctx.Value("user").(*User)
	if user.ActiveGame == nil {
		MessageReply(true, NotInGame).Send(conn)
		return
	}

	DataReply(false, IsOwner, user.ActiveGame.Owner == user).Send(conn)
}

func (g *GameSocketController) StartGame(ctx context.Context) {
	conn := ctx.Value("conn").(*websocket.Conn)

	user := ctx.Value("user").(*User)
	if user.ActiveGame == nil {
		MessageReply(true, NotInGame).Send(conn)
		return
	}

	if user.ActiveGame.Owner != user {
		MessageReply(true, NotOwner).Send(conn)
		return
	}

	if user.ActiveGame.Status != Standby {
		MessageReply(true, user.ActiveGame.Status).Send(conn)
		return
	}

	user.ActiveGame.Status = Starting

	n := g.GameTime
	for range time.Tick(time.Second * 1) {
		if n == 0 {
			break
		}
		for _, member := range user.ActiveGame.Members {
			DataReply(false, Starting, n).Send(member.Conn)
		}

		n -= 1
	}
	for id, member := range user.ActiveGame.Members {
		MessageReply(false, InProgress).Send(member.Conn)

		g.Session.NewSession(int(user.ActiveGame.Id), int(id), user.ActiveGame.InstId)
	}
	user.ActiveGame.Status = InProgress
	go g.PlayRounds(user.ActiveGame)

}

func (g *GameSocketController) ResetGame(ctx context.Context) {
	conn := ctx.Value("conn").(*websocket.Conn)

	user := ctx.Value("user").(*User)
	if user.ActiveGame == nil {
		MessageReply(true, NotInGame).Send(conn)
		return
	}

	if user.ActiveGame.Owner != user {
		MessageReply(true, NotOwner).Send(conn)
		return
	}

	if user.ActiveGame.Status != Finished {
		MessageReply(true, user.ActiveGame.Status).Send(conn)
		return
	}

	user.ActiveGame.Status = Standby
	user.ActiveGame.RoundStatus = RoundWaiting
	user.ActiveGame.CurrentRound = 0
	user.ActiveGame.Leaderboard = map[uint]float64{}
	user.ActiveGame.Rounds = map[int]*Round{}

	DataReply(false, ResetGame, user.ActiveGame).Send(conn)
}

type RoundData[T entity.Question | Question] struct {
	Timer    int `json:"timer"`
	Question *T  `json:"question"`
}

type Option struct {
	Name string `json:"name"`
}

type Question struct {
	Name    string   `json:"name"`
	Options []Option `json:"options"`
}

type FinishedReply struct {
	Correct     bool             `json:"correct"`
	Options     []*entity.Option `json:"options"`
	Leaderboard map[uint]float64 `json:"leaderboard"`
}

func (g *GameSocketController) PlayRounds(game *Game) {
	game.RoundStatus = RoundInProgress
	for {
		if game.Status == InProgress && game.RoundStatus == RoundInProgress {
			n := game.Data.RoundTime
			question := Question{}
			copier.Copy(&question, game.Data.Questions[game.CurrentRound])
			game.Rounds[game.CurrentRound] = &Round{Answers: map[uint]uint{}}

			for range time.Tick(time.Second * 1) {
				if n == 0 {
					for _, member := range game.Members {
						choice := game.Rounds[game.CurrentRound].Answers[member.Id]
						correct := game.Data.Questions[game.CurrentRound].Options[choice].Correct

						if correct {
							game.Leaderboard[member.Id] += game.Data.Points
						}
					}

					for _, member := range game.Members {
						choice := game.Rounds[game.CurrentRound].Answers[member.Id]
						correct := game.Data.Questions[game.CurrentRound].Options[choice].Correct

						DataReply(false, RoundFinished, FinishedReply{Correct: correct, Options: game.Data.Questions[game.CurrentRound].Options, Leaderboard: game.Leaderboard}).Send(member.Conn)
					}

					game.RoundStatus = RoundWaiting
					game.CurrentRound += 1

					break
				}
				for _, member := range game.Members {
					if game.Owner == member {
						DataReply(false, RoundInProgress, RoundData[entity.Question]{Question: game.Data.Questions[game.CurrentRound], Timer: n}).Send(member.Conn)
					} else {
						DataReply(false, RoundInProgress, RoundData[Question]{Question: &question, Timer: n}).Send(member.Conn)
					}
				}

				n -= 1
			}
		} else {
			if game.CurrentRound >= len(game.Data.Questions) {
				game.Status = Finished

				for id, member := range game.Members {
					DataReply(false, Finished, game.Leaderboard).Send(member.Conn)

					g.Session.EndSession(int(game.Id), int(id), game.InstId, game.QuestionCount, len(game.Members)-1, game.Leaderboard[id])
				}

				break
			}
		}
	}
}

type AnswerData struct {
	Option uint `json:"option"`
}

type AnswerResponse struct {
	UserId uint `json:"user"`
	Option uint `json:"option"`
}

func (g *GameSocketController) AnswerQuestion(ctx context.Context, msgData json.RawMessage) {
	user := ctx.Value("user").(*User)
	conn := ctx.Value("conn").(*websocket.Conn)

	var data AnswerData
	err := json.Unmarshal(msgData, &data)
	if err != nil {
		DataReply(true, "DATA_ERROR", err.Error()).Send(conn)
		return
	}

	if user.ActiveGame == nil {
		MessageReply(true, NotInGame).Send(conn)
		return
	}

	if user.ActiveGame.RoundStatus == RoundInProgress && user.ActiveGame.Status == InProgress {
		user.ActiveGame.Rounds[user.ActiveGame.CurrentRound].Answers[user.Id] = data.Option
		DataReply(false, AnswerAccepted, user.ActiveGame).Send(conn)
		DataReply(false, UserAnswered, AnswerResponse{
			UserId: user.Id,
			Option: data.Option,
		}).Send(user.ActiveGame.Owner.Conn)
	} else {
		MessageReply(true, RoundWaiting).Send(conn)
	}
}

func (g *GameSocketController) NextRound(ctx context.Context) {
	conn := ctx.Value("conn").(*websocket.Conn)

	user := ctx.Value("user").(*User)
	if user.ActiveGame == nil {
		MessageReply(true, NotInGame).Send(conn)
		return
	}

	if user.ActiveGame.Owner != user {
		MessageReply(true, NotOwner).Send(conn)
		return
	}

	if user.ActiveGame.Status != InProgress {
		MessageReply(true, user.ActiveGame.Status).Send(conn)
		return
	}

	if user.ActiveGame.RoundStatus != RoundWaiting {
		MessageReply(true, InProgress).Send(conn)
		return
	}

	user.ActiveGame.RoundStatus = RoundInProgress

	MessageReply(false, RoundInProgress).Send(conn)
}
