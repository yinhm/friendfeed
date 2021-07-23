package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/esimov/caire"
	"github.com/gofrs/uuid"
	"github.com/spf13/cobra"
	pb "github.com/yinhm/friendfeed/proto"
	"golang.org/x/net/context"
)

var wallpaper string

var wallpaperCmd = &cobra.Command{
	Use:   "wallpaper",
	Short: "download wallpaper from bing",
	Long:  `download daily 4k wallpaper from bing`,
	Run: func(cmd *cobra.Command, args []string) {
		err := downloadBingWallpaper()
		if err != nil {
			log.Println(err)
		}
	},
}

func init() {
	rootCmd.AddCommand(wallpaperCmd)
}

// def get_pic_url(self, rooturl, imgurlbase, fallbackurl, has_wp, resolution):
// 	wplink = webutil.urljoin(rooturl, '_'.join([imgurlbase, 'UHD.jpg']))
// 	_logger.debug('in UHD mode, get url %s', wplink)
// 	return wplink,

// }

const BASE_URL = "https://www.bing.com/HPImageArchive.aspx?format=js&idx=0&n=1&mkt=zh-CN"

type BingWallpaper struct {
	Images []struct {
		EndDate   string `json:"enddate"`
		UrlBase   string `json:"urlbase"`
		CopyRight string `json:"copyright"`
	}
}

func downloadBingWallpaper() error {
	uniqueName := fmt.Sprintf("bing:wallpaper")
	uuid1 := uuid.NewV5(uuid.NamespaceURL, strings.ToLower(uniqueName))

	feedinfo := &pb.Feedinfo{
		Uuid:        uuid1.String(),
		Id:          "bingwallpaper",
		Name:        "Bing Wallpaper",
		Type:        "sys",
		Private:     false,
		Description: "Bing Wallpaper",
	}
	profile, err := agent.client.PostFeedinfo(context.Background(), feedinfo)
	if err != nil {
		return err
	}

	resp, err := http.Get(BASE_URL)
	if err != nil {
		return err
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var bingWallpaper BingWallpaper

	err = json.Unmarshal(body, &bingWallpaper)
	if err != nil {
		return err
	}

	for _, img := range bingWallpaper.Images {
		url := fmt.Sprintf("http://cn.bing.com%s_UHD.jpg", img.UrlBase)
		log.Println(img.EndDate, url, img.CopyRight)

		resp, err := http.Get(url)
		if err != nil {
			return err
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		uniqueName := fmt.Sprintf("bing:wallpaper:%s", img.UrlBase)
		uuid1 := uuid.NewV5(uuid.NamespaceURL, strings.ToLower(uniqueName))
		outFile := fmt.Sprintf("%x", uuid1)
		outFile = outFile[:2] + "/" + outFile[2:]
		outFile = outFile[:1] + "/" + outFile[1:]
		log.Println("out file path: ", outFile)
		outFilepath := filepath.Join(config.datapath, "files", outFile)
		os.MkdirAll(filepath.Dir(outFilepath), 0755)
		os.WriteFile(outFile, body, 0755)

		// thumbnail
		p := &caire.Processor{
			NewWidth:  640,
			NewHeight: 640,
			Square:    true,
		}

		in, _ := os.Open(outFilepath)
		defer in.Close()

		outFileThumb := outFilepath + "-640"
		dst, err := os.OpenFile(outFileThumb, os.O_CREATE|os.O_WRONLY, 0755)
		defer dst.Close()

		if err := p.Process(in, dst); err != nil {
			fmt.Printf("Error rescaling image: %s", err.Error())
		}

		f1 := &pb.File{
			Name: img.CopyRight,
			Url:  "/file/" + outFile,
			Type: "image/jpeg",
		}
		f2 := &pb.Thumbnail{
			Link:   "/file/" + outFile,
			Url:    "/file/" + outFile + "-640",
			Width:  640,
			Height: 640,
		}

		// PostWallpaper
		dt := time.Now().UTC()
		entry := &pb.Entry{
			Id:         fmt.Sprintf("%x", uuid1),
			Date:       dt.Format(time.RFC3339),
			Files:      []*pb.File{f1},
			Thumbnails: []*pb.Thumbnail{f2},
		}

		from := &pb.Feed{
			Id:      profile.Id,
			Name:    profile.Name,
			Type:    profile.Type,
			Picture: profile.Picture,
		}
		entry.From = from
		// To:      []*pb.Feed{from},
		entry.ProfileUuid = profile.Uuid

		agent.client.PostEntry(context.Background(), entry)

		feedinfo.Picture = f2.Url
		agent.client.PostFeedinfo(context.Background(), feedinfo)

		log.Printf("同步 wallpaper 成功")
		return nil
	}

	return nil
}
