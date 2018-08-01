package profile

import (
	"bytes"
	"github.com/jchavannes/jgo/jerr"
	"github.com/memocash/memo/app/bitcoin/memo"
	"github.com/memocash/memo/app/cache"
	"github.com/memocash/memo/app/db"
	"github.com/memocash/memo/app/obj/rep"
	"github.com/memocash/memo/app/util"
	"github.com/memocash/memo/app/util/format"
	"strings"
	"time"
)

type Post struct {
	Name         string
	Memo         *db.MemoPost
	Parent       *Post
	Likes        []*Like
	HasLiked     bool
	SelfPkHash   []byte
	ReplyCount   uint
	Replies      []*Post
	Reputation   *rep.Reputation
	ShowMedia    bool
	Poll         *Poll
	VoteQuestion *db.MemoPost
	VoteOption   *db.MemoPollOption
	ProfilePic   *db.MemoSetPic
}

func (p Post) IsSelf() bool {
	if len(p.SelfPkHash) == 0 {
		return false
	}
	return bytes.Equal(p.SelfPkHash, p.Memo.PkHash)
}

func (p Post) IsLoggedIn() bool {
	return len(p.SelfPkHash) > 0
}

func (p Post) GetTotalTip() int64 {
	var totalTip int64
	for _, like := range p.Likes {
		totalTip += like.Amount
	}
	return totalTip
}

func (p Post) GetMessage() string {
	var msg = p.Memo.Message
	if p.ShowMedia {
		msg = format.AddYoutubeVideos(msg)
		msg = format.AddImgurImages(msg)
		msg = format.AddGiphyImages(msg)
		msg = format.AddTwitterImages(msg)
		msg = format.AddTweets(msg)
	}
	msg = strings.TrimSpace(msg)
	msg = format.AddLinks(msg)
	return msg
}

func (p Post) IsPoll() bool {
	if !p.Memo.IsPoll || p.Poll == nil {
		return false
	}
	numOptions := len(p.Poll.Question.Options)
	if numOptions >= 2 && int(p.Poll.Question.NumOptions) == numOptions {
		return true
	}
	return false
}

func (p Post) GetTimeString(timezone string) string {
	if p.Memo.BlockId != 0 {
		if p.Memo.Block != nil {
			return util.GetTimezoneTime(p.Memo.Block.Timestamp, timezone)
		} else {
			return "Unknown"
		}
	}
	return "Unconfirmed"
}

func (p Post) GetTimeAgo() string {
	if p.Memo.Block != nil && p.Memo.Block.Timestamp.Before(p.Memo.CreatedAt) {
		return util.GetTimeAgo(p.Memo.Block.Timestamp)
	} else {
		return util.GetTimeAgo(p.Memo.CreatedAt)
	}
}

func (p Post) GetLastLikeId() uint {
	var lastLikeId uint
	for _, like := range p.Likes {
		if like.Id > lastLikeId {
			lastLikeId = like.Id
		}
	}
	return lastLikeId
}

func GetPostsFeed(selfPkHash []byte, offset uint) ([]*Post, error) {
	dbPosts, err := db.GetPostsFeedForPkHash(selfPkHash, offset)
	if err != nil {
		return nil, jerr.Get("error getting posts for hash", err)
	}
	var foundPkHashes [][]byte
	for _, dbPost := range dbPosts {
		for _, foundPkHash := range foundPkHashes {
			if bytes.Equal(foundPkHash, dbPost.PkHash) {
				continue
			}
		}
		foundPkHashes = append(foundPkHashes, dbPost.PkHash)
	}
	names := make(map[string]string)
	for _, pkHash := range foundPkHashes {
		setName, err := db.GetNameForPkHash(pkHash)
		if err != nil && ! db.IsRecordNotFoundError(err) {
			return nil, jerr.Get("error getting name for hash", err)
		}
		if setName == nil {
			continue
		}
		names[string(pkHash)] = setName.Name
	}
	var posts []*Post
	for _, dbPost := range dbPosts {
		cnt, err := db.GetPostReplyCount(dbPost.TxHash)
		if err != nil {
			return nil, jerr.Get("error getting post reply count", err)
		}
		post := &Post{
			Memo:       dbPost,
			SelfPkHash: selfPkHash,
			ReplyCount: cnt,
		}
		name, ok := names[string(dbPost.PkHash)]
		if ok {
			post.Name = name
		}
		posts = append(posts, post)
	}
	return posts, nil
}

func GetPostsForHash(pkHash []byte, selfPkHash []byte, offset uint) ([]*Post, error) {
	var name = ""
	setName, err := db.GetNameForPkHash(pkHash)
	if err != nil {
		return nil, jerr.Get("error getting name for hash", err)
	}
	if setName != nil {
		name = setName.Name
	}
	setPic, err := db.GetPicForPkHash(pkHash)
	if err != nil {
		return nil, jerr.Get("error getting profile pic for hash", err)
	}
	dbPosts, err := db.GetPostsForPkHash(pkHash, offset)
	if err != nil {
		return nil, jerr.Get("error getting posts for hash", err)
	}
	var posts []*Post
	for _, dbPost := range dbPosts {
		cnt, err := db.GetPostReplyCount(dbPost.TxHash)
		if err != nil {
			return nil, jerr.Get("error getting post reply count", err)
		}
		post := &Post{
			Name:       name,
			Memo:       dbPost,
			SelfPkHash: selfPkHash,
			ReplyCount: cnt,
		}
		if setPic != nil {
			post.ProfilePic = setPic
		}
		posts = append(posts, post)
	}
	return posts, nil
}

func GetPostByTxHashWithReplies(txHash []byte, selfPkHash []byte, offset uint) (*Post, error) {
	memoPost, err := db.GetMemoPost(txHash)
	if err != nil {
		return nil, jerr.Get("error getting memo post", err)
	}
	setName, err := db.GetNameForPkHash(memoPost.PkHash)
	if err != nil {
		return nil, jerr.Get("error getting name for hash", err)
	}
	var name = ""
	if setName != nil {
		name = setName.Name
	}
	setPic, err := db.GetPicForPkHash(memoPost.PkHash)
	if err != nil {
		return nil, jerr.Get("error getting profile pic for hash", err)
	}
	cnt, err := db.GetPostReplyCount(txHash)
	if err != nil {
		return nil, jerr.Get("error getting post reply count", err)
	}
	post := &Post{
		Name:       name,
		Memo:       memoPost,
		SelfPkHash: selfPkHash,
		ReplyCount: cnt,
	}
	if setPic != nil {
		post.ProfilePic = setPic
	}
	err = AttachRepliesToPost(post, offset)
	if err != nil {
		return nil, jerr.Get("error attaching replies to post", err)
	}
	return post, nil
}

func GetPostByTxHash(txHash []byte, selfPkHash []byte) (*Post, error) {
	memoPost, err := db.GetMemoPost(txHash)
	if err != nil {
		return nil, jerr.Get("error getting memo post", err)
	}
	setName, err := db.GetNameForPkHash(memoPost.PkHash)
	if err != nil {
		return nil, jerr.Get("error getting name for hash", err)
	}
	var name = ""
	if setName != nil {
		name = setName.Name
	}
	cnt, err := db.GetPostReplyCount(txHash)
	if err != nil {
		return nil, jerr.Get("error getting post reply count", err)
	}
	post := &Post{
		Name:       name,
		Memo:       memoPost,
		SelfPkHash: selfPkHash,
		ReplyCount: cnt,
	}
	return post, nil
}

func GetPostsByTxHashes(txHashes [][]byte, selfPkHash []byte) ([]*Post, error) {
	memoPosts, err := db.GetPostsByTxHashes(txHashes)
	if err != nil {
		return nil, jerr.Get("error getting memo posts", err)
	}
	var namePkHashes [][]byte
	var pollVoteTxHashes [][]byte
	for _, memoPost := range memoPosts {
		namePkHashes = append(namePkHashes, memoPost.PkHash)
		if memoPost.IsVote {
			pollVoteTxHashes = append(pollVoteTxHashes, memoPost.TxHash)
		}
	}
	memoPollVotes, err := db.GetMemoPollVotesByTxHashes(pollVoteTxHashes)
	if err != nil {
		return nil, jerr.Get("error getting poll votes by tx hashes", err)
	}
	var pollOptionTxHashes [][]byte
	for _, memoPollVote := range memoPollVotes {
		pollOptionTxHashes = append(pollOptionTxHashes, memoPollVote.OptionTxHash)
	}
	memoPollOptions, err := db.GetMemoPollOptionsByTxHashes(pollOptionTxHashes)
	if err != nil {
		return nil, jerr.Get("error getting poll options by tx hashes", err)
	}
	var pollTxHashes [][]byte
	for _, memoPollOption := range memoPollOptions {
		pollTxHashes = append(pollTxHashes, memoPollOption.PollTxHash)
	}
	memoPollQuestions, err := db.GetPostsByTxHashes(pollTxHashes)
	if err != nil {
		return nil, jerr.Get("error getting poll questions for tx hashes", err)
	}
	setNames, err := db.GetNamesForPkHashes(namePkHashes)
	if err != nil {
		return nil, jerr.Get("error getting names for hashes", err)
	}
	txHashCounts, err := db.GetPostReplyCounts(txHashes)
	if err != nil {
		return nil, jerr.Get("error getting post reply counts", err)
	}
	var posts []*Post
	for _, memoPost := range memoPosts {
		var name string
		var replyCount uint
		var voteQuestion *db.MemoPost
		var voteOption *db.MemoPollOption
		for _, setName := range setNames {
			if bytes.Equal(setName.PkHash, memoPost.PkHash) {
				name = setName.Name
			}
		}
		for _, txHashCount := range txHashCounts {
			if bytes.Equal(txHashCount.TxHash, memoPost.TxHash) {
				replyCount = txHashCount.Count
			}
		}
		if memoPost.IsVote {
			for _, memoPollVote := range memoPollVotes {
				if bytes.Equal(memoPollVote.TxHash, memoPost.TxHash) {
					for _, memoPollOption := range memoPollOptions {
						if bytes.Equal(memoPollOption.TxHash, memoPollVote.OptionTxHash) {
							voteOption = memoPollOption
							for _, memoPollQuestion := range memoPollQuestions {
								if bytes.Equal(memoPollQuestion.TxHash, memoPollOption.PollTxHash) {
									voteQuestion = memoPollQuestion
								}
							}
						}
					}
				}
			}
		}
		posts = append(posts, &Post{
			Name:         name,
			Memo:         memoPost,
			SelfPkHash:   selfPkHash,
			ReplyCount:   replyCount,
			VoteQuestion: voteQuestion,
			VoteOption:   voteOption,
		})
	}
	return posts, nil
}

func AttachRepliesToPost(post *Post, offset uint) error {
	replyMemoPosts, err := db.GetPostReplies(post.Memo.TxHash, offset)
	if err != nil {
		return jerr.Get("error getting post replies", err)
	}
	var replies []*Post
	for _, reply := range replyMemoPosts {
		setName, err := db.GetNameForPkHash(reply.PkHash)
		if err != nil {
			return jerr.Get("error getting name for reply hash", err)
		}
		var name = ""
		if setName != nil {
			name = setName.Name
		}
		setPic, err := db.GetPicForPkHash(reply.PkHash)
		if err != nil {
			return jerr.Get("error getting profile pic for hash", err)
		}
		cnt, err := db.GetPostReplyCount(reply.TxHash)
		if err != nil {
			return jerr.Get("error getting post reply count", err)
		}
		var post = &Post{
			Name:       name,
			Memo:       reply,
			SelfPkHash: post.SelfPkHash,
			ReplyCount: cnt,
		}
		if setPic != nil {
			post.ProfilePic = setPic
		}
		replies = append(replies, post)
	}
	post.Replies = replies
	return nil
}

func GetRecentPosts(selfPkHash []byte, offset uint) ([]*Post, error) {
	dbPosts, err := db.GetRecentPosts(offset)
	if err != nil {
		return nil, jerr.Get("error getting posts for hash", err)
	}
	posts, err := CreatePostsFromDbPosts(selfPkHash, dbPosts)
	if err != nil {
		return nil, jerr.Get("error creating posts from db posts", err)
	}
	err = AttachNamesToPosts(posts)
	if err != nil {
		return nil, jerr.Get("error attaching names to posts", err)
	}
	err = AttachProfilePicsToPosts(posts)
	if err != nil {
		return nil, jerr.Get("error attaching profile pics to posts", err)
	}
	return posts, nil
}

func GetTopPostsNamedRange(selfPkHash []byte, offset uint, timeRange string, personalized bool) ([]*Post, error) {
	var timeStart time.Time
	switch timeRange {
	case TimeRange1Hour:
		timeStart = time.Now().Add(-1 * time.Hour)
	case TimeRange24Hours:
		timeStart = time.Now().Add(-24 * time.Hour)
	case TimeRange7Days:
		timeStart = time.Now().Add(-24 * 7 * time.Hour)
	case TimeRangeAll:
		timeStart = time.Now().Add(-24 * 365 * 10 * time.Hour)
	}
	return GetTopPosts(selfPkHash, offset, timeStart, time.Time{}, personalized)
}

func GetRankedPosts(selfPkHash []byte, offset uint) ([]*Post, error) {
	memoPosts, err := db.GetRankedPosts(offset)
	if err != nil {
		return nil, jerr.Get("error getting posts for hash", err)
	}
	posts, err := CreatePostsFromDbPosts(selfPkHash, memoPosts)
	if err != nil {
		return nil, jerr.Get("error creating posts from db posts", err)
	}
	err = AttachNamesToPosts(posts)
	if err != nil {
		return nil, jerr.Get("error attaching names to posts", err)
	}
	err = AttachProfilePicsToPosts(posts)
	if err != nil {
		return nil, jerr.Get("error attaching profile pics to posts", err)
	}
	return posts, nil
}

func GetPollsPosts(selfPkHash []byte, offset uint) ([]*Post, error) {
	memoPosts, err := db.GetPollsPosts(offset)
	if err != nil {
		return nil, jerr.Get("error getting posts for hash", err)
	}
	posts, err := CreatePostsFromDbPosts(selfPkHash, memoPosts)
	if err != nil {
		return nil, jerr.Get("error creating posts from db posts", err)
	}
	err = AttachNamesToPosts(posts)
	if err != nil {
		return nil, jerr.Get("error attaching names to posts", err)
	}
	err = AttachProfilePicsToPosts(posts)
	if err != nil {
		return nil, jerr.Get("error attaching profile pics to posts", err)
	}
	return posts, nil
}

func CreatePostsFromDbPosts(selfPkHash []byte, dbPosts []*db.MemoPost) ([]*Post, error) {
	var posts []*Post
	for _, dbPost := range dbPosts {
		cnt, err := db.GetPostReplyCount(dbPost.TxHash)
		if err != nil {
			return nil, jerr.Get("error getting post reply count", err)
		}
		post := &Post{
			Memo:       dbPost,
			SelfPkHash: selfPkHash,
			ReplyCount: cnt,
		}
		posts = append(posts, post)
	}
	return posts, nil
}

func AttachNamesToPosts(posts []*Post) error {
	var namePkHashes [][]byte
	for _, post := range posts {
		for _, namePkHash := range namePkHashes {
			if bytes.Equal(namePkHash, post.Memo.PkHash) {
				continue
			}
		}
		namePkHashes = append(namePkHashes, post.Memo.PkHash)
	}
	setNames, err := db.GetNamesForPkHashes(namePkHashes)
	if err != nil {
		return jerr.Get("error getting set names for pk hashes", err)
	}
	for _, setName := range setNames {
		for _, post := range posts {
			if bytes.Equal(post.Memo.PkHash, setName.PkHash) {
				post.Name = setName.Name
			}
		}
	}
	return nil
}

func AttachProfilePicsToPosts(posts []*Post) error {
	var namePkHashes [][]byte
	for _, post := range posts {
		for _, namePkHash := range namePkHashes {
			if bytes.Equal(namePkHash, post.Memo.PkHash) {
				continue
			}
		}
		namePkHashes = append(namePkHashes, post.Memo.PkHash)
	}
	setPics, err := db.GetPicsForPkHashes(namePkHashes)
	if err != nil {
		return jerr.Get("error getting profile pics for pk hashes", err)
	}
	for _, setPic := range setPics {
		for _, post := range posts {
			if bytes.Equal(post.Memo.PkHash, setPic.PkHash) {
				post.ProfilePic = setPic
			}
		}
	}
	return nil
}

func GetTopPosts(selfPkHash []byte, offset uint, timeStart time.Time, timeEnd time.Time, personalized bool) ([]*Post, error) {
	var dbPosts []*db.MemoPost
	var err error
	if personalized {
		dbPosts, err = db.GetPersonalizedTopPosts(selfPkHash, offset, timeStart, timeEnd)
		if err != nil {
			return nil, jerr.Get("error getting posts for hash", err)
		}
	} else {
		dbPosts, err = db.GetTopPosts(offset, timeStart, timeEnd)
		if err != nil {
			return nil, jerr.Get("error getting posts for hash", err)
		}
	}
	posts, err := CreatePostsFromDbPosts(selfPkHash, dbPosts)
	if err != nil {
		return nil, jerr.Get("error creating posts from db posts", err)
	}
	err = AttachNamesToPosts(posts)
	if err != nil {
		return nil, jerr.Get("error attaching names to posts", err)
	}
	err = AttachProfilePicsToPosts(posts)
	if err != nil {
		return nil, jerr.Get("error attaching profile pics to posts", err)
	}
	return posts, nil
}

func GetPostsForTopic(tag string, selfPkHash []byte, offset uint) ([]*Post, error) {
	dbPosts, err := db.GetPostsForTopic(tag, offset)
	if err != nil {
		return nil, jerr.Get("error getting posts for hash", err)
	}
	posts, err := CreatePostsFromDbPosts(selfPkHash, dbPosts)
	if err != nil {
		return nil, jerr.Get("error creating posts from db posts", err)
	}
	err = AttachNamesToPosts(posts)
	if err != nil {
		return nil, jerr.Get("error attaching names to posts", err)
	}
	err = AttachProfilePicsToPosts(posts)
	if err != nil {
		return nil, jerr.Get("error attaching profile pics to posts", err)
	}
	return posts, nil
}

func GetOlderPostsForTopic(tag string, selfPkHash []byte, firstPostId uint) ([]*Post, error) {
	dbPosts, err := db.GetOlderPostsForTopic(tag, firstPostId)
	if err != nil {
		return nil, jerr.Get("error getting posts", err)
	}
	posts, err := CreatePostsFromDbPosts(selfPkHash, dbPosts)
	if err != nil {
		return nil, jerr.Get("error creating posts from db posts", err)
	}
	err = AttachNamesToPosts(posts)
	if err != nil {
		return nil, jerr.Get("error attaching names to posts", err)
	}
	err = AttachProfilePicsToPosts(posts)
	if err != nil {
		return nil, jerr.Get("error attaching profile pics to posts", err)
	}
	return posts, nil
}

func AttachReplyCountToPosts(posts []*Post) error {
	var txHashes [][]byte
	for _, post := range posts {
		txHashes = append(txHashes, post.Memo.TxHash)
	}
	txHashCounts, err := db.GetPostReplyCounts(txHashes)
	if err != nil {
		return jerr.Get("error getting post reply counts", err)
	}
	for _, txHashCount := range txHashCounts {
		for _, post := range posts {
			if bytes.Equal(post.Memo.TxHash, txHashCount.TxHash) {
				post.ReplyCount = txHashCount.Count
			}
		}
	}
	return nil
}

func AttachParentToPosts(posts []*Post) error {
	for _, post := range posts {
		if len(post.Memo.ParentTxHash) == 0 {
			continue
		}
		parentPost, err := db.GetMemoPost(post.Memo.ParentTxHash)
		if err != nil {
			jerr.Get("error getting memo post parent", err).Print()
			continue
		}
		setName, err := db.GetNameForPkHash(parentPost.PkHash)
		if err != nil {
			return jerr.Get("error getting name for reply hash", err)
		}
		var name = ""
		if setName != nil {
			name = setName.Name
		}
		setPic, err := db.GetPicForPkHash(parentPost.PkHash)
		if err != nil {
			return jerr.Get("error getting profile pic for hash", err)
		}
		post.Parent = &Post{
			Name:       name,
			Memo:       parentPost,
			SelfPkHash: post.SelfPkHash,
		}
		if setPic != nil {
			post.Parent.ProfilePic = setPic
		}
	}
	return nil
}

func SetShowMediaForPosts(posts []*Post, userId uint) error {
	if userId == 0 {
		for _, post := range posts {
			post.ShowMedia = true
			if post.Parent != nil {
				post.Parent.ShowMedia = true
			}
		}
		return nil
	}
	settings, err := cache.GetUserSettings(userId)
	if err != nil {
		return jerr.Get("error getting user settings", err)
	}
	if settings.Integrations == db.SettingIntegrationsAll {
		for _, post := range posts {
			post.ShowMedia = true
			if post.Parent != nil {
				post.Parent.ShowMedia = true
			}
		}
	}
	return nil
}

func AttachPollsToPosts(posts []*Post) error {
	for _, post := range posts {
		if post.Memo.IsPoll {
			question, err := db.GetMemoPollQuestion(post.Memo.TxHash)
			if err != nil {
				return jerr.Get("error getting memo poll question", err)
			}
			numOptions := len(question.Options)
			if numOptions < 2 || int(question.NumOptions) != numOptions {
				continue
			}
			post.Poll = &Poll{
				Question:   question,
				SelfPkHash: post.SelfPkHash,
			}
			single := question.PollType == memo.CodePollTypeSingle
			votes, err := db.GetVotesForOptions(question.TxHash, single)
			if err != nil {
				if db.IsRecordNotFoundError(err) {
					continue
				}
				return jerr.Get("error getting votes for options", err)
			}
			post.Poll.Votes = votes
		}
		if post.Memo.IsVote {
			memoPollVote, err := db.GetMemoPollVote(post.Memo.TxHash)
			if err != nil {
				return jerr.Get("error getting memo poll vote", err)
			}
			memoPollOption, err := db.GetMemoPollOption(memoPollVote.OptionTxHash)
			if err != nil {
				return jerr.Get("error getting memo poll option", err)
			}
			post.VoteOption = memoPollOption
			memoPost, err := db.GetMemoPost(memoPollOption.PollTxHash)
			if err != nil {
				return jerr.Get("error getting memo poll question post", err)
			}
			post.VoteQuestion = memoPost
		}
	}
	return nil
}
