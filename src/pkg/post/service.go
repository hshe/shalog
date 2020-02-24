package post

import (
	"context"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/icowan/blog/src/config"
	"github.com/icowan/blog/src/middleware"
	"github.com/icowan/blog/src/repository"
	"github.com/icowan/blog/src/repository/types"
	"github.com/mozillazg/go-pinyin"
	"github.com/pkg/errors"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidArgument = errors.New("invalid argument")
	ErrPostCreate      = errors.New("发布失败 ")
	ErrPostFind        = errors.New("查询失败")
	ErrPostUpdate      = errors.New("更新失败")
	ErrPostParams      = errors.New("参数错误")
)

type Service interface {
	// 详情页信息
	Get(ctx context.Context, id int64) (rs map[string]interface{}, err error)

	// 列表页
	List(ctx context.Context, order, by, category string, pageSize, offset int) (rs []map[string]interface{}, count int64, other map[string]interface{}, err error)

	// 受欢迎的
	Popular(ctx context.Context) (rs []map[string]interface{}, err error)

	// 更新点赞
	Awesome(ctx context.Context, id int64) (err error)

	// 搜索文章
	Search(ctx context.Context, keyword, tag string, categoryId int64, offset, pageSize int) (posts []*types.Post, total int64, err error)

	// 创建新文章
	NewPost(ctx context.Context, title, description, content string,
		postStatus repository.PostStatus, categoryIds, tagIds []int64, markdown bool, imageId int64) (err error)

	// 编辑内容 ps: 参数意思就不写了,变量名称就是意思...
	Put(ctx context.Context, id int64, title, description, content string,
		postStatus repository.PostStatus, categoryIds, tagIds []int64, markdown bool, imageId int64) (err error)

	// 删除文章
	Delete(ctx context.Context, id int64) (err error)

	// 恢复文章
	Restore(ctx context.Context, id int64) (err error)
}

type service struct {
	repository repository.Repository
	logger     log.Logger
	config     *config.Config
}

func (c *service) Restore(ctx context.Context, id int64) (err error) {
	post, err := c.repository.Post().FindOnce(id)
	if err != nil {
		_ = level.Error(c.logger).Log("repository.Post", "FindOnce", "err", err.Error())
		return errors.Wrap(err, ErrPostFind.Error())
	}

	post.DeletedAt = nil

	err = c.repository.Post().Update(post)
	if err != nil {
		_ = level.Error(c.logger).Log("repository.Post", "Update", "err", err.Error())
		err = errors.Wrap(err, ErrPostUpdate.Error())
	}
	return
}

func (c *service) Delete(ctx context.Context, id int64) (err error) {
	post, err := c.repository.Post().FindOnce(id)
	if err != nil {
		_ = level.Error(c.logger).Log("repository.Post", "FindOnce", "err", err.Error())
		return errors.Wrap(err, ErrPostFind.Error())
	}

	t := time.Now()
	post.DeletedAt = &t

	err = c.repository.Post().Update(post)
	if err != nil {
		_ = level.Error(c.logger).Log("repository.Post", "Update", "err", err.Error())
		err = errors.Wrap(err, ErrPostUpdate.Error())
	}

	return nil
}

func (c *service) Put(ctx context.Context, id int64, title, description, content string,
	postStatus repository.PostStatus, categoryIds, tagIds []int64, markdown bool, imageId int64) (err error) {

	// todo: 是否需要验证是否为文章本人编辑呢？
	// userId, _ := ctx.Value(middleware.ContextUserId).(int64)

	post, err := c.repository.Post().FindOnce(id)
	if err != nil {
		_ = level.Error(c.logger).Log("repository.Post", "FindOnce", "err", err.Error())
		return errors.Wrap(err, ErrPostFind.Error())
	}

	categories, err := c.repository.Category().FindByIds(categoryIds)
	if err != nil {
		_ = level.Error(c.logger).Log("repository.Category", "FindByIds", "err", err.Error())
		return
	}
	tags, err := c.repository.Tag().FindByIds(tagIds)
	if err != nil {
		_ = level.Error(c.logger).Log("repository.Tag", "FindByIds", "err", err.Error())
		return
	}

	if post.PushTime == nil && postStatus == repository.PostStatusPublish {
		t := time.Now()
		post.PushTime = &t
	} else {
		post.PushTime = nil
	}

	// 清除分类关系表数据
	//_ = c.repository.Category().CleanByPostId(post.ID)

	// 清除tag关系表数据
	//_ = c.repository.Tag().CleanByPostId(post.ID)

	// 清除images的关系数据

	post.Title = title
	post.Description = description
	post.Content = content
	post.PostStatus = postStatus.String()
	post.Categories = categories
	post.Tags = tags

	var imageExists bool
	for _, v := range post.Images {
		if v.ID == imageId {
			imageExists = true
			break
		}
	}

	if !imageExists {
		if images, e := c.repository.Image().FindByPostIds([]int64{imageId}); e == nil {
			var imgs []types.Image
			for _, v := range images {
				imgs = append(imgs, *v)
			}
			post.Images = imgs
		}
	}

	err = c.repository.Post().Update(post)
	if err != nil {
		_ = level.Error(c.logger).Log("repository.Post", "Update", "err", err.Error())
		err = errors.Wrap(err, ErrPostUpdate.Error())
	}

	return
}

func (c *service) NewPost(ctx context.Context, title, description, content string,
	postStatus repository.PostStatus, categoryIds, tagIds []int64, markdown bool, imageId int64) (err error) {

	userId, _ := ctx.Value(middleware.ContextUserId).(int64)

	user, err := c.repository.User().FindById(userId)
	if err != nil {
		_ = level.Error(c.logger).Log("repository.User", "FindById", "id", "id", "err", err.Error())
		return
	}

	categories, err := c.repository.Category().FindByIds(categoryIds)
	if err != nil {
		_ = level.Error(c.logger).Log("repository.Category", "FindByIds", "err", err.Error())
		return
	}
	tags, err := c.repository.Tag().FindByIds(tagIds)
	if err != nil {
		_ = level.Error(c.logger).Log("repository.Tag", "FindByIds", "err", err.Error())
		return
	}

	var pushTime *time.Time
	if postStatus == repository.PostStatusPublish {
		t := time.Now()
		pushTime = &t
	}

	var slug string
	// todo: 没有Gcc 好像不太好使，windows需要自行安装, mac,linux下好像自带
	slug = strings.Join(pinyin.LazyConvert(title, nil), "-")

	// todo: 如果数据不全均为草稿

	post := types.Post{
		Content:     content,
		Description: description,
		Slug:        slug,     // todo: 在transport转换成拼音
		IsMarkdown:  markdown, // todo: 考虑传参进来
		ReadNum:     1,
		Reviews:     1,
		Awesome:     1,
		Title:       title,
		UserID:      userId,
		PostStatus:  postStatus.String(),
		PushTime:    pushTime,
		User:        user,
		Tags:        tags,
		Categories:  categories,
	}

	if err = c.repository.Post().Create(&post); err != nil {
		err = errors.Wrap(err, ErrPostCreate.Error())
	}

	return
}

func (c *service) Search(ctx context.Context, keyword, tag string, categoryId int64, offset, pageSize int) (posts []*types.Post, total int64, err error) {
	if keyword != "" {
		return c.repository.Post().Search(keyword, categoryId, offset, pageSize)
	}

	if tag != "" {
		tagInfo, err := c.repository.Tag().FindPostIdsByName(tag)
		if err != nil {
			_ = level.Warn(c.logger).Log("repository.Tag", "FindPostByName", "err", err.Error())
			return nil, 0, nil
		}

		return c.repository.Post().FindByIds(tagInfo.PostIds, categoryId, offset, pageSize)
	}

	return
}

func (c *service) Awesome(ctx context.Context, id int64) (err error) {
	post, err := c.repository.Post().FindOnce(id)
	if err != nil {
		return
	}
	post.Awesome += 1
	return c.repository.Post().Update(post)
}

func (c *service) Get(ctx context.Context, id int64) (rs map[string]interface{}, err error) {
	detail, err := c.repository.Post().Find(id)
	if err != nil {
		return
	}

	if detail == nil {
		return nil, repository.PostNotFound
	}

	var headerImage string

	if image, err := c.repository.Image().FindByPostIdLast(id); err == nil && image != nil {
		headerImage = c.config.GetString("server", "image_domain") + "/" + image.ImagePath
	}

	var category types.Category
	for _, v := range detail.Categories {
		category = v
		break
	}

	// prev
	prev, _ := c.repository.Post().Prev(detail.PushTime, []int64{category.Id})
	// next
	next, _ := c.repository.Post().Next(detail.PushTime, []int64{category.Id})

	populars, _ := c.Popular(ctx)
	return map[string]interface{}{
		"content":      detail.Content,
		"title":        detail.Title,
		"publish_at":   detail.PushTime.Format("2006/01/02 15:04:05"),
		"updated_at":   detail.UpdatedAt,
		"author":       detail.User.Username,
		"comment":      detail.Reviews,
		"banner_image": headerImage,
		"read_num":     strconv.Itoa(int(detail.ReadNum)),
		"description":  strings.Replace(detail.Description, "\n", "", -1),
		"tags":         detail.Tags,
		"populars":     populars,
		"prev":         prev,
		"next":         next,
		"awesome":      detail.Awesome,
		"id":           detail.ID,
	}, nil
}

func (c *service) List(ctx context.Context, order, by, category string, pageSize, offset int) (rs []map[string]interface{},
	count int64, other map[string]interface{}, err error) {
	// 取列表 判断搜索、分类、Tag条件
	// 取最多阅读

	var posts []types.Post
	if category != "" {
		if category, total, err := c.repository.Category().FindByName(category, pageSize, offset); err == nil {
			for _, v := range category.Posts {
				posts = append(posts, v)
			}
			count = total
		}
	} else {
		var categoryIds []int64
		if cates, err := c.repository.Category().FindAll(); err == nil {
			for _, v := range cates {
				categoryIds = append(categoryIds, v.Id)
			}
		}
		posts, count, err = c.repository.Post().FindBy(categoryIds, order, by, pageSize, offset)
		if err != nil {
			_ = level.Warn(c.logger).Log("repository.Post", "FindBy", "err", err.Error())
			return
		}
	}

	var postIds []int64
	for _, post := range posts {
		postIds = append(postIds, post.ID)
	}

	images, err := c.repository.Image().FindByPostIds(postIds)
	if err == nil && images == nil {
		_ = level.Warn(c.logger).Log("c.image.FindByPostIds", "postIds", "err", err)
	}

	imageMap := make(map[int64]string, len(images))
	for _, image := range images {
		imageMap[image.PostID] = imageUrl(image.ImagePath, c.config.GetString("server", "image_domain"))
	}

	_ = c.logger.Log("count", count)

	for _, val := range posts {
		imageUrl, ok := imageMap[val.ID]
		if !ok {
			_ = c.logger.Log("postId", val.ID, "image", ok)
		}
		rs = append(rs, map[string]interface{}{
			"id":         strconv.FormatUint(uint64(val.ID), 10),
			"title":      val.Title,
			"desc":       val.Description,
			"publish_at": val.PushTime.Format("2006/01/02 15:04:05"),
			"image_url":  imageUrl,
			"comment":    val.Reviews,
			"author":     val.User.Username,
			"tags":       val.Tags,
		})
	}

	tags, _ := c.repository.Tag().List(20)

	populars, _ := c.Popular(ctx)
	other = map[string]interface{}{
		"tags":     tags,
		"populars": populars,
		"category": category,
	}

	return
}

func (c *service) Popular(ctx context.Context) (rs []map[string]interface{}, err error) {

	posts, err := c.repository.Post().Popular()
	if err != nil {
		return
	}

	var postIds []int64

	for _, post := range posts {
		postIds = append(postIds, post.ID)
	}

	images, err := c.repository.Image().FindByPostIds(postIds)
	if err == nil && images == nil {
		_ = c.logger.Log("c.image.FindByPostIds", "postIds", "err", err)
	}

	imageMap := make(map[int64]string, len(images))
	for _, image := range images {
		imageMap[image.PostID] = imageUrl(image.ImagePath, c.config.GetString("server", "image_domain"))
	}

	for _, post := range posts {
		imageUrl, ok := imageMap[post.ID]
		if !ok {
			_ = c.logger.Log("postId", post.ID, "image", ok)
		}

		desc := []rune(post.Description)
		rs = append(rs, map[string]interface{}{
			"title":     post.Title,
			"desc":      string(desc[:40]),
			"id":        post.ID,
			"image_url": imageUrl,
		})
	}

	return
}

func imageUrl(path, imageDomain string) string {
	return imageDomain + "/" + path
}

func NewService(logger log.Logger, cf *config.Config, repository repository.Repository) Service {
	return &service{
		repository: repository,
		logger:     logger,
		config:     cf,
	}
}