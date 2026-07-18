package cmd

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/yinhm/friendfeed/media"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/util"
	"golang.org/x/net/context"
)

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

const BASE_URL = "https://www.bing.com/HPImageArchive.aspx?format=js&idx=0&n=10&mkt=zh-CN"

type BingImage struct {
	FullStartDate string `json:"fullstartdate"`
	EndDate       string `json:"enddate"`
	UrlBase       string `json:"urlbase"`
	CopyRight     string `json:"copyright"`
}

type BingWallpaper struct {
	Images []BingImage
}

func downloadBingWallpaper() error {
	cfg := &util.Config{
		MediaPath: viper.GetString("media_path"),
	}
	mfs := media.NewLocalStorage(cfg, 640)

	uuid1 := model.UniqueKeyFrom("bing", "wallpaper")

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
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var bingWallpaper BingWallpaper

	err = json.Unmarshal(body, &bingWallpaper)
	if err != nil {
		return err
	}

	// reverse images order
	for i, j := 0, len(bingWallpaper.Images)-1; i < j; i, j = i+1, j-1 {
		bingWallpaper.Images[i], bingWallpaper.Images[j] = bingWallpaper.Images[j], bingWallpaper.Images[i]
	}

	for _, img := range bingWallpaper.Images {
		url := fmt.Sprintf("http://cn.bing.com%s_UHD.jpg", img.UrlBase)
		log.Println(img.EndDate, url, img.CopyRight)

		uuid1 := model.UniqueKeyFrom("bing", "wallpaper", img.UrlBase)
		outFile := fmt.Sprintf("%x", uuid1)
		if found, _ := mfs.Exists(outFile); found {
			log.Printf("File exists, skipping %s...", img.EndDate)
			continue
		}

		obj := &media.Object{
			Filename: outFile,
			Url:      url,
		}

		log.Println("fetching image: ", url)
		if _, err := mfs.Fetch(obj); err != nil {
			return err
		}

		// write file
		if _, err = mfs.Post(obj); err != nil {
			return err
		}
		thumbObj, err := mfs.Thumbnail(obj)
		if err != nil {
			return fmt.Errorf("create thumbnail for %s: %w", img.EndDate, err)
		}

		f1 := &pb.File{
			Name: img.CopyRight,
			Url:  "/file/" + obj.Path,
			Type: "image/jpeg",
		}
		f2 := &pb.Thumbnail{
			Link:   "/file/" + obj.Path,
			Url:    "/file/" + thumbObj.Path,
			Width:  thumbObj.Width,
			Height: thumbObj.Height,
		}

		// PostWallpaper
		fullTime := fmt.Sprintf("%s-%s-%sT%s:00:00+08:00",
			img.FullStartDate[:4],
			img.FullStartDate[4:6],
			img.FullStartDate[6:8],
			img.FullStartDate[8:10])
		dt, err := time.Parse(time.RFC3339, fullTime)
		if err != nil {
			log.Println(err, fullTime)
			dt = time.Now()
			dt = time.Date(dt.Year(), dt.Month(), dt.Day(), 0, 0, 0, 0, time.UTC)
		}

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

		log.Printf("同步 %s wallpaper 成功", dt.Format(time.RFC3339))
	}

	return nil
}

var OldWallpapers = BingWallpaper{
	Images: []BingImage{
		{EndDate: "2021-07-231600", CopyRight: "The Minokake-Iwa rocks off the coast of the Izu Peninsula, Japan (© Krzysztof Baranowski/Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.MinokakeRocks_EN-US9026307089_UHD.jpg"},
		{EndDate: "2021-07-221600", CopyRight: "Wachsenburg Castle near Erfurt, Germany (© Radius Images/Offset by Shutterstock)", UrlBase: "https://cn.bing.com/th?id=OHR.WachsenburgCastle_EN-US8953512968_UHD.jpg"},
		{EndDate: "2021-07-211600", CopyRight: "Composite image of the moon (© Prathamesh Jaju)", UrlBase: "https://cn.bing.com/th?id=OHR.PrathameshJaju_EN-US8876008160_UHD.jpg"},
		{EndDate: "2021-07-201600", CopyRight: "Colorful alleyway in the medina of Tétouan, Morocco (© Jan Wlodarczyk/eStock Photo)", UrlBase: "https://cn.bing.com/th?id=OHR.Tetouan_EN-US7379560261_UHD.jpg"},
		{EndDate: "2021-07-191600", CopyRight: "Tour de France riders in front of the Louvre Pyramid and Museum in Paris, France, during the 2020 race (© Martin Bureau/AFP via Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.LouvreRiders_EN-US7293709223_UHD.jpg"},
		{EndDate: "2021-07-181600", CopyRight: "A Loepa oberthuri moth (© Robert Thompson/Minden Pictures)", UrlBase: "https://cn.bing.com/th?id=OHR.LoepaOberthuri_EN-US7208560265_UHD.jpg"},
		{EndDate: "2021-07-171600", CopyRight: "Mont Choisy Beach, Mauritius (© Robert Harding World Imagery/Offset by Shutterstock)", UrlBase: "https://cn.bing.com/th?id=OHR.MontChoisy_EN-US7121697055_UHD.jpg"},
		{EndDate: "2021-07-161600", CopyRight: "Boats float by rice fields on the Ngo Dong River in Ninh Bình province, Vietnam (© Jeremy Woodhouse/Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.NgoDong_EN-US7569222084_UHD.jpg"},
		{EndDate: "2021-07-151600", CopyRight: "Blacktip reef sharks off the coast of Tahiti, French Polynesia (© Paul Mckenzie/Minden Pictures)", UrlBase: "https://cn.bing.com/th?id=OHR.SharkAwareness_EN-US7444020818_UHD.jpg"},
		{EndDate: "2021-07-141600", CopyRight: "Moose crossing a pond below Mount Moran, Grand Teton National Park, Wyoming (© Jim Stamates/Minden Pictures)", UrlBase: "https://cn.bing.com/th?id=OHR.MooseVelvet_EN-US7292213302_UHD.jpg"},
		{EndDate: "2021-07-131600", CopyRight: "Wave crashing on Farolim de Felgueiras, a lighthouse in Porto, Portugal (© Stephan Zirwes/Offset by Shutterstock)", UrlBase: "https://cn.bing.com/th?id=OHR.LighthouseWave_EN-US6948276315_UHD.jpg"},
		{EndDate: "2021-07-121600", CopyRight: "Spiral aloe (© David Madison/Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.SpiralAloe_EN-US6880291357_UHD.jpg"},
		{EndDate: "2021-07-111600", CopyRight: "Milky Way over the Tagus River in Monfragüe National Park, Spain (© Miguel Angel Muñoz Ruiz/Cavan Images)", UrlBase: "https://cn.bing.com/th?id=OHR.MonfragueNationalPark_EN-US6445504463_UHD.jpg"},
		{EndDate: "2021-07-101600", CopyRight: "Ortygia, a small island off the coast of Syracuse, Sicily, Italy (© DaLiu/Shutterstock)", UrlBase: "https://cn.bing.com/th?id=OHR.Ortygia_EN-US5940165843_UHD.jpg"},
		{EndDate: "2021-07-091600", CopyRight: "The Appalachian Trail in Stokes State Forest, New Jersey (© Frank DeBonis/Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.AppalachianTrail_EN-US5662298732_UHD.jpg"},
		{EndDate: "2021-07-081600", CopyRight: "Kazem Dashi rock formation in Lake Urmia, Iran (© Ali/Adobe Stock)", UrlBase: "https://cn.bing.com/th?id=OHR.LakeUrmia_EN-US4986086287_UHD.jpg"},
		{EndDate: "2021-07-071600", CopyRight: "Tawny frogmouth chick, Australia (© SnapRapid/Offset by Shutterstock)", UrlBase: "https://cn.bing.com/th?id=OHR.TawnyFrogmouth_EN-US4707407967_UHD.jpg"},
		{EndDate: "2021-07-061600", CopyRight: "Serra da Malagueta mountains on Santiago Island, Cabo Verde (© Samuel Borges Photography/Shutterstock)", UrlBase: "https://cn.bing.com/th?id=OHR.SerraMalagueta_EN-US4627693270_UHD.jpg"},
		{EndDate: "2021-07-051600", CopyRight: "Fireworks in San Francisco, California (© tampatra/Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.SFFireworks_EN-US4561699680_UHD.jpg"},
		{EndDate: "2021-07-041600", CopyRight: "Wakatobi National Park, Indonesia (© Fabio Lamanna/Alamy)", UrlBase: "https://cn.bing.com/th?id=OHR.WakatobiNP_EN-US4475854788_UHD.jpg"},
		{EndDate: "2021-07-031600", CopyRight: "A meerkat in Namibia (© Danita Delimont/Offset by Shutterstock)", UrlBase: "https://cn.bing.com/th?id=OHR.ShyFive_EN-US4337641438_UHD.jpg"},
		{EndDate: "2021-07-021600", CopyRight: "'Passage migratoire' ('Migratory Passage'), an art installation by Giorgia Volpe in Old Québec City, Québec, Canada (© Lucbouch/Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.HangingCanoes_EN-US0235160370_UHD.jpg"},
		{EndDate: "2021-07-011600", CopyRight: "Manicouagan Crater in Québec, Canada (© Universal History Archive/Universal Images Group via Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.Manicouagan_EN-US7701393606_UHD.jpg"},
		{EndDate: "2021-06-301600", CopyRight: "Rocks on Anse Source d'Argent beach, La Digue Island, Seychelles (© Roland Gerth/eStock Photo)", UrlBase: "https://cn.bing.com/th?id=OHR.RocksSeychelles_EN-US7406548278_UHD.jpg"},
		{EndDate: "2021-06-291600", CopyRight: "The Cittadella on the island of Gozo, Malta (© Davide Seddio/Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.Cittadella_EN-US6067516722_UHD.jpg"},
		{EndDate: "2021-06-281600", CopyRight: "Lincoln Center for the Performing Arts lit in Pride colors on June 18, 2020 in New York City (© Alexi Rosenfeld/Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.LCPAPride_EN-US5979726065_UHD.jpg"},
		{EndDate: "2021-06-271600", CopyRight: "Glass sightseeing platform in Shilinxia Scenic Area, Pinggu District of Beijing, China (© STR/AFP via Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.Shilinxia_EN-US5445196689_UHD.jpg"},
		{EndDate: "2021-06-261600", CopyRight: "Empress brilliant hummingbird and a bee in Colombia (© Jiri Hrebicek/Alamy)", UrlBase: "https://cn.bing.com/th?id=OHR.Heliodoxa_EN-US5338295561_UHD.jpg"},
		{EndDate: "2021-06-251600", CopyRight: "Caribou in Denali National Park and Preserve, Alaska (© Design Pics/Danita Delimont)", UrlBase: "https://cn.bing.com/th?id=OHR.DenaliCaribou_EN-US5229911845_UHD.jpg"},
		{EndDate: "2021-06-241600", CopyRight: "Fireflies in Nichinan, Tottori, Japan (© north-tail/Getty Images Plus)", UrlBase: "https://cn.bing.com/th?id=OHR.Nichinan_EN-US5055695100_UHD.jpg"},
		{EndDate: "2021-06-231600", CopyRight: "Seljalandsfoss waterfall in the South Region of Iceland (© Tom Mackie/plainpicture)", UrlBase: "https://cn.bing.com/th?id=OHR.SouthCoast_EN-US4824290612_UHD.jpg"},
		{EndDate: "2021-06-221600", CopyRight: "Rothschild's giraffe in Lake Nakuru National Park, Kenya (© Theo Allofs/Minden Pictures)", UrlBase: "https://cn.bing.com/th?id=OHR.RothschildGiraffe_EN-US4621962761_UHD.jpg"},
		{EndDate: "2021-06-211600", CopyRight: "Bald eagle pair with a chick in their nest near the Yukon River, Yukon, Canada (© Mark Newman/Minden Pictures)", UrlBase: "https://cn.bing.com/th?id=OHR.FatherEagle_EN-US4516693152_UHD.jpg"},
		{EndDate: "2021-06-201600", CopyRight: "People surfing at Burleigh Heads, Gold Coast, Australia (© Vicki Smith/Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.BurleighHeads_EN-US4425800469_UHD.jpg"},
		{EndDate: "2021-06-191600", CopyRight: "Aerial view of Chapel Bridge over the River Reuss in Lucerne, Switzerland (© Neleman Initiative/Gallery Stock)", UrlBase: "https://cn.bing.com/th?id=OHR.ReussRiver_EN-US4195043036_UHD.jpg"},
		{EndDate: "2021-06-181600", CopyRight: "Bright Eye sea cave on the Nā Pali Coast, Kauai, Hawaii (© jimkruger/Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.BrightEye_EN-US9581825024_UHD.jpg"},
		{EndDate: "2021-06-171600", CopyRight: "Green sea turtle diving, Great Barrier Reef, Queensland, Australia (© imageBROKER/Alamy)", UrlBase: "https://cn.bing.com/th?id=OHR.GBRTurtle_EN-US9472992921_UHD.jpg"},
		{EndDate: "2021-06-161600", CopyRight: "Aerial view of volcanic Lake Pinatubo and mountains, Luzon, Philippines (© Amazing Aerial Agency/Offset by Shutterstock)", UrlBase: "https://cn.bing.com/th?id=OHR.LakePinatubo_EN-US8170111215_UHD.jpg"},
		{EndDate: "2021-06-151600", CopyRight: "The George Washington Bridge displays the American flag in honor of Flag Day, June 14, 2016, Fort Lee, New Jersey (© Robert D. Barnes/Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.LargestFlag_EN-US9248418324_UHD.jpg"},
		{EndDate: "2021-06-141600", CopyRight: "Eurasian brown bear cub in the taiga forest, Finland (© Jules Cox/Minden Pictures)", UrlBase: "https://cn.bing.com/th?id=OHR.FinlandBrownBear_EN-US9193102113_UHD.jpg"},
		{EndDate: "2021-06-131600", CopyRight: "View of the Rio Grande in Big Bend National Park, Texas (© Ian Shive/Tandem Stills + Motion)", UrlBase: "https://cn.bing.com/th?id=OHR.BBNPGrande_EN-US9017603902_UHD.jpg"},
		{EndDate: "2021-06-121600", CopyRight: "Small loch in Glen Etive, Scotland (© Oliver Hellowell/Minden Pictures)", UrlBase: "https://cn.bing.com/th?id=OHR.GlenEtive_EN-US8902001915_UHD.jpg"},
		{EndDate: "2021-06-111600", CopyRight: "Nossa Senhora da Graça Fort near Elvas, Portugal (© Luis Pina Photography/Shutterstock)", UrlBase: "https://cn.bing.com/th?id=OHR.ForteNossa_EN-US8946379841_UHD.jpg"},
		{EndDate: "2021-06-101600", CopyRight: "Annular eclipse over New Mexico, May 20, 2012 (© ssucsy/Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.AnnularEclipse_EN-US8858263866_UHD.jpg"},
		{EndDate: "2021-06-091600", CopyRight: "Thousands of jack fish swimming together at Cabo Pulmo National Park, Sea of Cortez, Baja California, Mexico (© Christian Vizl/Tandem Stills + Motion)", UrlBase: "https://cn.bing.com/th?id=OHR.CortezJacks_EN-US4025428525_UHD.jpg"},
		{EndDate: "2021-06-081600", CopyRight: "An indigo bunting on a sunflower (© William Krumpelman/Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.BuntingBird_EN-US8373607335_UHD.jpg"},
		{EndDate: "2021-06-071600", CopyRight: "Mulberry harbour at Arromanches-les-Bains, Normandy, France (© agefotostock/Alamy)", UrlBase: "https://cn.bing.com/th?id=OHR.ArromanchesLesBains_EN-US8268306845_UHD.jpg"},
		{EndDate: "2021-06-061600", CopyRight: "Black-mandibled toucan in the rainforest canopy of La Selva Biological Station in Costa Rica (© Greg Basco/Minden Pictures)", UrlBase: "https://cn.bing.com/th?id=OHR.ToucanRainforest_EN-US8174584515_UHD.jpg"},
		{EndDate: "2021-06-051600", CopyRight: "Eastern Island and Spit Island, Midway Atoll (© Ian Shive/Tandem Stills + Motion)", UrlBase: "https://cn.bing.com/th?id=OHR.EasternIsland_EN-US7992088058_UHD.jpg"},
		{EndDate: "2021-06-041600", CopyRight: "Cyclists on a wooden suspension bridge over the Soča River in Slovenia (© Amazing Aerial Agency/Offset by Shutterstock)", UrlBase: "https://cn.bing.com/th?id=OHR.SocaCycles_EN-US8987262585_UHD.jpg"},
		{EndDate: "2021-06-031600", CopyRight: "Springboks near a waterhole in Etosha National Park, Namibia (© Charlie Summers/Minden Pictures)", UrlBase: "https://cn.bing.com/th?id=OHR.EstoshaSpringbok_EN-US8878416660_UHD.jpg"},
		{EndDate: "2021-06-021600", CopyRight: "Aerial view of the Grotta della Poesia (Poetry's Cave) near Roca, Lecce, Italy (© Amazing Aerial Agency/Offset by Shutterstock)", UrlBase: "https://cn.bing.com/th?id=OHR.PoetrysCave_EN-US8786875244_UHD.jpg"},
		{EndDate: "2021-06-011600", CopyRight: "Military Women's Memorial, located at the gateway to Arlington National Cemetery, Virginia (© Brycia James/Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.WomensMemorial_EN-US8561851319_UHD.jpg"},
		{EndDate: "2021-05-311600", CopyRight: "California sea lion in a forest of giant kelp near the Channel Islands of California (© Nature Picture Library/Alamy)", UrlBase: "https://cn.bing.com/th?id=OHR.SeaDog_EN-US8346901369_UHD.jpg"},
		{EndDate: "2021-05-301600", CopyRight: "Alley and bamboo grove in Wuhou Temple, Chengdu, Sichuan province, China (© Eastimages/Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.RedAlley_EN-US8215991251_UHD.jpg"},
		{EndDate: "2021-05-291600", CopyRight: "Robin's nest with a brown-headed cowbird egg (© Edward Kinsman/Science Photo Library)", UrlBase: "https://cn.bing.com/th?id=OHR.CowbirdsEgg_EN-US8103879720_UHD.jpg"},
		{EndDate: "2021-05-281600", CopyRight: "'I Can Hear It,' an installation by artist Ivars Drulle on the beach by the villages of Middelkerke and Westende, Belgium (© Arterra Picture Library/Alamy)", UrlBase: "https://cn.bing.com/th?id=OHR.ICanHearIt_EN-US7945824197_UHD.jpg"},
		{EndDate: "2021-05-271600", CopyRight: "The total lunar eclipse of April 4, 2015, photographed over Monument Valley, Utah (© Alan Dyer/Alamy)", UrlBase: "https://cn.bing.com/th?id=OHR.TearDropEclipse_EN-US7861293677_UHD.jpg"},
		{EndDate: "2021-05-261600", CopyRight: "Sperm whale off the coast of Roseau, Dominica, in the Caribbean Sea (© Tony Wu/Minden Pictures)", UrlBase: "https://cn.bing.com/th?id=OHR.TowelDay_EN-US7748070759_UHD.jpg"},
		{EndDate: "2021-05-251600", CopyRight: "The Infinite Bridge in Aarhus, Denmark (© Kosmaj/Shutterstock)", UrlBase: "https://cn.bing.com/th?id=OHR.AarhusInfinite_EN-US7607613784_UHD.jpg"},
		{EndDate: "2021-05-241600", CopyRight: "The renovated Rose Main Reading Room, New York Public Library Main Branch, New York City (© Sascha Kilmer/Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.RoseRoom_EN-US7194472524_UHD.jpg"},
		{EndDate: "2021-05-231600", CopyRight: "The medieval walled town in Tossa de Mar, Catalonia, Spain (© dleiva/Alamy)", UrlBase: "https://cn.bing.com/th?id=OHR.CapeofTossa_EN-US6969132211_UHD.jpg"},
		{EndDate: "2021-05-221600", CopyRight: "Whooping cranes taking off during spring migration in South Dakota (© Gerrit Vyn/Minden Pictures)", UrlBase: "https://cn.bing.com/th?id=OHR.WhoopingCranes_EN-US5576295451_UHD.jpg"},
		{EndDate: "2021-05-211600", CopyRight: "A bee dives into a lotus flower at Kenilworth Park and Aquatic Gardens in Washington, DC (© Linda Davidson/The Washington Post via Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.BeeLotus_EN-US7861856689_UHD.jpg"},
		{EndDate: "2021-05-201600", CopyRight: "Fallen rhododendron petals line a trail through Pisgah National Forest, North Carolina (© aheflin/Getty Images Plus)", UrlBase: "https://cn.bing.com/th?id=OHR.RoanRhododendron_EN-US8777664012_UHD.jpg"},
		{EndDate: "2021-05-191600", CopyRight: "Centre Pompidou Málaga in Málaga, Spain (© Wim Wiskerke/Alamy)", UrlBase: "https://cn.bing.com/th?id=OHR.PompidouMalaga_EN-US7695811401_UHD.jpg"},
		{EndDate: "2021-05-181600", CopyRight: "Ålesund, Norway (© AWL Images/Offset by Shutterstock)", UrlBase: "https://cn.bing.com/th?id=OHR.Alesund_EN-US7597098434_UHD.jpg"},
		{EndDate: "2021-05-171600", CopyRight: "Aerial view of El Peñón de Guatapé, Guatapé, Antioquia, Colombia (© Amazing Aerial Agency/Offset by Shutterstock)", UrlBase: "https://cn.bing.com/th?id=OHR.Guatape_EN-US7463341939_UHD.jpg"},
		{EndDate: "2021-05-161600", CopyRight: "Telescopes and star trails at Paranal Observatory, Atacama Desert, Chile (© Matteo Omied/Alamy)", UrlBase: "https://cn.bing.com/th?id=OHR.ParanalStars_EN-US4851647464_UHD.jpg"},
		{EndDate: "2021-05-151600", CopyRight: "Amazon rainforest with morning fog near Alta Floresta, Mato Grosso, Brazil (© Pulsar Imagens/Alamy)", UrlBase: "https://cn.bing.com/th?id=OHR.AltaFloresta_EN-US4736416258_UHD.jpg"},
		{EndDate: "2021-05-141600", CopyRight: "Shikisai no Oka flower gardens in Biei, Japan (© Tanya Jones/Shutterstock)", UrlBase: "https://cn.bing.com/th?id=OHR.ShikisaiBiei_EN-US4615475287_UHD.jpg"},
		{EndDate: "2021-05-131600", CopyRight: "A view across the River Shannon in Limerick, County Limerick, Ireland (© Piotr Machowczyk/Shutterstock)", UrlBase: "https://cn.bing.com/th?id=OHR.LimerickDay_EN-US4512689467_UHD.jpg"},
		{EndDate: "2021-05-121600", CopyRight: "Grinnell Lake, Glacier National Park, Montana (© Pung/Shutterstock)", UrlBase: "https://cn.bing.com/th?id=OHR.GrinnellGlacier_EN-US4427090483_UHD.jpg"},
		{EndDate: "2021-05-111600", CopyRight: "The Hōkūle'a, a traditional Hawaiian voyaging canoe, departs for a 3-year voyage from Honolulu, Hawaii, on May 17, 2014 (© Reuters/Alamy)", UrlBase: "https://cn.bing.com/th?id=OHR.Hokulea_EN-US8698576653_UHD.jpg"},
		{EndDate: "2021-05-101600", CopyRight: "Sea otter mother and newborn pup, Monterey Bay, California (© Suzi Eszterhas/Minden Pictures)", UrlBase: "https://cn.bing.com/th?id=OHR.OtterMom_EN-US8059433484_UHD.jpg"},
		{EndDate: "2021-05-091600", CopyRight: "Black-tailed godwits, Netherlands (© Edward van Altena/Minden Pictures)", UrlBase: "https://cn.bing.com/th?id=OHR.LimosaLimosa_EN-US4076563094_UHD.jpg"},
		{EndDate: "2021-05-081600", CopyRight: "Norcross Brook and wetlands near Moosehead Lake in Piscataquis County, Maine (© Aaron Black-Schmidt/Tandem Stills + Motion)", UrlBase: "https://cn.bing.com/th?id=OHR.MaineWetland_EN-US3940841989_UHD.jpg"},
		{EndDate: "2021-05-071600", CopyRight: "'Now & Forever,' a mural by Tristan Eaton honoring health care workers, May 11, 2020, in New York City (© Timothy A. Clary/AFP via Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.NurseMask_EN-US2085492290_UHD.jpg"},
		{EndDate: "2021-05-061600", CopyRight: "The Great Pyramid of Cholula, in Cholula, Puebla, Mexico (© mauritius images GmbH/Alamy)", UrlBase: "https://cn.bing.com/th?id=OHR.Cholula_EN-US2015612893_UHD.jpg"},
		{EndDate: "2021-05-051600", CopyRight: "Grey seal hitching itself over the beach at Donna Nook, North Lincolnshire, England (© Frederic Desmette/Minden Pictures)", UrlBase: "https://cn.bing.com/th?id=OHR.StarWarsSeal_EN-US1938844381_UHD.jpg"},
		{EndDate: "2021-05-041600", CopyRight: "Poster for Teacher Appreciation Week by 12-year-old Caroline Holt, 7th-grade student at the Bush School, Seattle, Washington (© Caroline Holt/Eugenia Chang)", UrlBase: "https://cn.bing.com/th?id=OHR.TeacherHeart_EN-US1874465116_UHD.jpg"},
		{EndDate: "2021-05-031600", CopyRight: "Burchell's zebra stallions, Rietvlei Nature Reserve, South Africa (© Richard Du Toit/Minden Pictures)", UrlBase: "https://cn.bing.com/th?id=OHR.LaughingZebras_EN-US1800178960_UHD.jpg"},
		{EndDate: "2021-05-021600", CopyRight: "Cherry blossoms at the Japanese Tea Garden in Golden Gate Park, San Francisco, California (© luisascanio/iStock/Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.GGTeaGarden_EN-US1647173456_UHD.jpg"},
		{EndDate: "2021-05-011600", CopyRight: "'The Spirit of Harlem' mural by Louis Delsarte in Harlem, New York City (© Pietro Scozzari/agefotostock)", UrlBase: "https://cn.bing.com/th?id=OHR.SpiritHarlem_EN-US1474494856_UHD.jpg"},
		{EndDate: "2021-04-301600", CopyRight: "Aerial view of tidal channels in marshland of the Mockhorn Island State Wildlife Management Area, Virginia (© Shane Gross/Minden Pictures)", UrlBase: "https://cn.bing.com/th?id=OHR.Mockhorn_EN-US1360987065_UHD.jpg"},
		{EndDate: "2021-04-291600", CopyRight: "Northern gannets on Great Saltee Island, Ireland (© Danny Green/Minden Pictures)", UrlBase: "https://cn.bing.com/th?id=OHR.GannetsSaltee_EN-US1285648780_UHD.jpg"},
		{EndDate: "2021-04-281600", CopyRight: "Yayoi Kusama's 'Pumpkin' artwork on Naoshima Island, Japan, in August 2018 (© Wirestock/Alamy)", UrlBase: "https://cn.bing.com/th?id=OHR.KusamaPumpkin_EN-US1211132220_UHD.jpg"},
		{EndDate: "2021-04-271600", CopyRight: "Wensleydale, Yorkshire Dales National Park, North Yorkshire, England (© Guy Edwardes/Minden Pictures)", UrlBase: "https://cn.bing.com/th?id=OHR.Wensleydale_EN-US0930314934_UHD.jpg"},
		{EndDate: "2021-04-261600", CopyRight: "Adélie penguins diving off an iceberg in Antarctica (© Mike Hill/Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.AdelieDiving_EN-US0845944074_UHD.jpg"},
		{EndDate: "2021-04-251600", CopyRight: "The Cholla Cactus Garden in Joshua Tree National Park, California (© Bryan Jolley/Tandem Stills + Motion)", UrlBase: "https://cn.bing.com/th?id=OHR.ChollaGarden_EN-US0706816050_UHD.jpg"},
		{EndDate: "2021-04-241600", CopyRight: "Casa Batlló in Barcelona, Catalonia, Spain (© Marco Arduino/Sime/eStock Photo)", UrlBase: "https://cn.bing.com/th?id=OHR.BatlloJordi_EN-US0619227174_UHD.jpg"},
		{EndDate: "2021-04-231600", CopyRight: "Mississippi River on the border between Arkansas and Mississippi (© NASA)", UrlBase: "https://cn.bing.com/th?id=OHR.MississippiRiver_EN-US2192534174_UHD.jpg"},
		{EndDate: "2021-04-221600", CopyRight: "The north coast of Madeira, Portugal (© Hemis/Alamy)", UrlBase: "https://cn.bing.com/th?id=OHR.SaoJorgeMadeira_EN-US8002002726_UHD.jpg"},
		{EndDate: "2021-04-211600", CopyRight: "Tegallalang Rice Terraces, Ubud, Bali, Indonesia (© Michele Falzone/Alamy)", UrlBase: "https://cn.bing.com/th?id=OHR.Ceking_EN-US7899895685_UHD.jpg"},
		{EndDate: "2021-04-201600", CopyRight: "Large school of Munk's devil rays seen from the air, Gulf of California, Mexico (© Mark Carwardine/Minden Pictures)", UrlBase: "https://cn.bing.com/th?id=OHR.Mobula_EN-US7757384682_UHD.jpg"},
		{EndDate: "2021-04-191600", CopyRight: "Montalbano Elicona, Messina, Sicily, Italy (© Antonino Bartuccio/SOPA Collection/Offset by Shutterstock)", UrlBase: "https://cn.bing.com/th?id=OHR.MontalbanoElicona_EN-US7629651237_UHD.jpg"},
		{EndDate: "2021-04-181600", CopyRight: "New River Gorge Bridge in the New River Gorge National Park and Preserve, West Virginia (© Entropy Workshop/iStock/Getty Images Plus)", UrlBase: "https://cn.bing.com/th?id=OHR.NewRiverGorge_EN-US7524399883_UHD.jpg"},
		{EndDate: "2021-04-171600", CopyRight: "Dalí Theatre-Museum in Figueres, Spain (© Valerija Polakovska/Shutterstock)", UrlBase: "https://cn.bing.com/th?id=OHR.DaliMuseum_EN-US9901160892_UHD.jpg"},
		{EndDate: "2021-04-161600", CopyRight: "Jackie Robinson signs autographs at spring training in Ciudad Trujillo, now Santo Domingo, Dominican Republic, on March 6, 1948 (© Bettmann/Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.JackieRobinson_EN-US7103495692_UHD.jpg"},
		{EndDate: "2021-04-151600", CopyRight: "Wildflowers in the Carrizo Plain National Monument, California (© Dennis Frates/Alamy)", UrlBase: "https://cn.bing.com/th?id=OHR.CarrizoPlain_EN-US7034817036_UHD.jpg"},
		{EndDate: "2021-04-141600", CopyRight: "Wat Phra Si Sanphet, Ayutthaya Historical Park, Ayutthaya, Thailand (© travelstock44/Alamy)", UrlBase: "https://cn.bing.com/th?id=OHR.WatPhraSiSanphet_EN-US6931344989_UHD.jpg"},
		{EndDate: "2021-04-131600", CopyRight: "Earth viewed from the International Space Station, photographed by astronaut Jeff Williams (© Jeff Williams/NASA)", UrlBase: "https://cn.bing.com/th?id=OHR.YurisNight_EN-US6858652982_UHD.jpg"},
		{EndDate: "2021-04-121600", CopyRight: "Mount Yoshino, Nara Prefecture, Japan (© Sean Pavone/iStock/Getty Images Plus)", UrlBase: "https://cn.bing.com/th?id=OHR.YoshinoyamaSpring_EN-US6772406506_UHD.jpg"},
		{EndDate: "2021-04-111600", CopyRight: "Grizzly bear cub siblings playing in Denali National Park and Preserve, Alaska (© Ron Niebrugge/Alamy)", UrlBase: "https://cn.bing.com/th?id=OHR.SiblingBears_EN-US6609087772_UHD.jpg"},
		{EndDate: "2021-04-101600", CopyRight: "Square Tower Group in Hovenweep National Monument, Utah (© Brad McGinley Photography/Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.HovenweepDarkSky_EN-US6328400931_UHD.jpg"},
		{EndDate: "2021-04-091600", CopyRight: "Black grouse male calling at a lek site in Kuusamo, Finland (© Oliver Smart/Alamy)", UrlBase: "https://cn.bing.com/th?id=OHR.TetraoTetrix_EN-US8933698445_UHD.jpg"},
		{EndDate: "2021-04-081600", CopyRight: "Willow tree in early spring, Minnesota (© Jim Brandenburg/Minden Pictures)", UrlBase: "https://cn.bing.com/th?id=OHR.WillowNewGrowth_EN-US3318398276_UHD.jpg"},
		{EndDate: "2021-04-071600", CopyRight: "The Acropolis of Athens, Greece (© Lucky-photographer/Shutterstock)", UrlBase: "https://cn.bing.com/th?id=OHR.Olympics125_EN-US8602188549_UHD.jpg"},
		{EndDate: "2021-04-061600", CopyRight: "Saut du Brot stone bridge in the Areuse Gorge, Neuchâtel, Switzerland (© Andreas Gerth/eStock Photo)", UrlBase: "https://cn.bing.com/th?id=OHR.SautduBrot_EN-US8410506080_UHD.jpg"},
		{EndDate: "2021-04-051600", CopyRight: "An Ostereierbaum (Easter egg tree) in Saalfeld, Germany (© Rudi Sebastian/Alamy)", UrlBase: "https://cn.bing.com/th?id=OHR.EggTree_EN-US8284116541_UHD.jpg"},
		{EndDate: "2021-04-041600", CopyRight: "Lighthouse at Cape Aniva, Sakhalin Island, Russia (© Amazing Aerial Agency/Offset by Shutterstock)", UrlBase: "https://cn.bing.com/th?id=OHR.AnivaLighthouse_EN-US8147045989_UHD.jpg"},
		{EndDate: "2021-04-031600", CopyRight: "Lençóis Maranhenses National Park in the state of Maranhão, Brazil (© WIN-Initiative/Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.BrazilSandDunes_EN-US8030598740_UHD.jpg"},
		{EndDate: "2021-04-021600", CopyRight: "Common chia elephant (Loxodonta laprofolis) in stealth stance, Marakele National Park, Limpopo, South Africa (© Staffan Widstrand/Minden Pictures)", UrlBase: "https://cn.bing.com/th?id=OHR.ShyGuy_EN-US7880739914_UHD.jpg"},
		{EndDate: "2021-04-011600", CopyRight: "Raja Ampat, an archipelago in Indonesia (© Amazing Aerial Agency/Offset by Shutterstock)", UrlBase: "https://cn.bing.com/th?id=OHR.RajaAmpat_EN-US7737563013_UHD.jpg"},
		{EndDate: "2021-03-311600", CopyRight: "Detail of an ostrich fern in spring, Washington state (© Stephen Matera/Tandem Stills + Motion)", UrlBase: "https://cn.bing.com/th?id=OHR.SwordFern_EN-US7523587413_UHD.jpg"},
		{EndDate: "2021-03-301600", CopyRight: "Reynisdrangar (basalt rock formations) on Reynisfjara Beach, Iceland (© Cavan Images/Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.Reynisfjara_EN-US7429542895_UHD.jpg"},
		{EndDate: "2021-03-291600", CopyRight: "The Jefferson Memorial during the National Cherry Blossom Festival in Washington, DC (© Rae Gabrielle/Alamy)", UrlBase: "https://cn.bing.com/th?id=OHR.JeffersonCherries_EN-US7147255858_UHD.jpg"},
		{EndDate: "2021-03-281600", CopyRight: "Mountain hare running across snow-covered upland, Scotland (© SCOTLAND: The Big Picture/Minden Pictures)", UrlBase: "https://cn.bing.com/th?id=OHR.MadHares_EN-US7045432576_UHD.jpg"},
		{EndDate: "2021-03-271600", CopyRight: "Cradle Mountain-Lake St. Clair National Park, Tasmania, Australia (© Paparwin Tanupatarachai/Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.MTCradle_EN-US6777988781_UHD.jpg"},
		{EndDate: "2021-03-261600", CopyRight: "Ancient Roman gold mining site of Las Médulas, León province, Spain (© David Santiago Garcia/Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.GoldMine_EN-US9932494168_UHD.jpg"},
		{EndDate: "2021-03-251600", CopyRight: "Humpback whale mother pushes her sleeping calf to the surface, Maui, Hawaii (© Ralph Pace/Minden Pictures)", UrlBase: "https://cn.bing.com/th?id=OHR.HumpbackMom_EN-US9862782184_UHD.jpg"},
		{EndDate: "2021-03-241600", CopyRight: "Satellite image of the Mania River in Madagascar (© NASA Earth Observatory image by Joshua Stevens, using Landsat data from the US Geological Survey)", UrlBase: "https://cn.bing.com/th?id=OHR.LoftedMadagascar_EN-US9720623596_UHD.jpg"},
		{EndDate: "2021-03-231600", CopyRight: "Tuskegee Airmen reading a map (© Bettmann/Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.TuskegeeAirmen_EN-US9643365119_UHD.jpg"},
		{EndDate: "2021-03-221600", CopyRight: "Bluebell flowers carpet the Hallerbos forest floor, Flanders, Belgium (© Jason Langley/plainpicture)", UrlBase: "https://cn.bing.com/th?id=OHR.HallesWood_EN-US9545891830_UHD.jpg"},
		{EndDate: "2021-03-211600", CopyRight: "Sundial on Parnidis Dune, Curonian Spit, Lithuania (© amoklv/Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.ParnidisSundial_EN-US9491593439_UHD.jpg"},
		{EndDate: "2021-03-201600", CopyRight: "Aerial view of the City of Adelaide shipwreck with trees growing on it, Magnetic Island, Queensland, Australia (© Amazing Aerial Agency/Offset by Shutterstock)", UrlBase: "https://cn.bing.com/th?id=OHR.MagneticIsland_EN-US9412695841_UHD.jpg"},
		{EndDate: "2021-03-191600", CopyRight: "Mount Etna erupting in 2013, Sicily, Italy (© Wead/Alamy Live News)", UrlBase: "https://cn.bing.com/th?id=OHR.MtEtna_EN-US8761813954_UHD.jpg"},
		{EndDate: "2021-03-181600", CopyRight: "Inisheer, the smallest of the three Aran Islands, in Galway Bay, Ireland (© Chris Hill/Minden Pictures)", UrlBase: "https://cn.bing.com/th?id=OHR.Inisheer_EN-US8680602205_UHD.jpg"},
		{EndDate: "2021-03-171600", CopyRight: "Giant panda cub at Bifengxia Panda Base, Sichuan, China (© Suzi Eszterhas/Minden Pictures)", UrlBase: "https://cn.bing.com/th?id=OHR.BifengxiaPanda_EN-US8585443782_UHD.jpg"},
		{EndDate: "2021-03-161600", CopyRight: "Screech owl resting in a tree cavity, Massapequa Preserve, Long Island, New York (© Vicki Jauron, Babylon and Beyond Photography/Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.MassapequaOwl_EN-US8469635086_UHD.jpg"},
		{EndDate: "2021-03-151600", CopyRight: "Astronomical clock, Lyon, France (© kyolshin/Alamy)", UrlBase: "https://cn.bing.com/th?id=OHR.LyonAstronomical_EN-US8367377789_UHD.jpg"},
		{EndDate: "2021-03-141600", CopyRight: "Common rhododendrons in Semper Forest Park, Rügen, Germany (© Sandra Bartocha/Minden Pictures)", UrlBase: "https://cn.bing.com/th?id=OHR.Rhododendron_EN-US8246366006_UHD.jpg"},
		{EndDate: "2021-03-131600", CopyRight: "A balloon flies over the Pyramid of the Sun at sunrise in Teotihuacan, Mexico (© Marco Ugarte/AP Photo)", UrlBase: "https://cn.bing.com/th?id=OHR.AztecNewYear_EN-US8147148173_UHD.jpg"},
		{EndDate: "2021-03-121600", CopyRight: "Thor's Well at Cape Perpetua on the Oregon coast (© Cavan Images/Offset by Shutterstock)", UrlBase: "https://cn.bing.com/th?id=OHR.CapePerpetua_EN-US1381606733_UHD.jpg"},
		{EndDate: "2021-03-111600", CopyRight: "'Step on Board,' the Harriet Tubman Memorial, sculpted by Fern Cunningham, in Boston, Massachusetts (© Anthony Pleva/Alamy)", UrlBase: "https://cn.bing.com/th?id=OHR.HarrietTubman_EN-US1054261891_UHD.jpg"},
		{EndDate: "2021-03-101600", CopyRight: "Foothills of the Diablo Range in the East Bay region of Northern California (© Jeff Lewis/Tandem Stills + Motion)", UrlBase: "https://cn.bing.com/th?id=OHR.RollingHills_EN-US0930573674_UHD.jpg"},
		{EndDate: "2021-03-091600", CopyRight: "View of the Notorious RBG mural by the street artist Elle in New York City (© lev radin/Alamy)", UrlBase: "https://cn.bing.com/th?id=OHR.NotoriousRBG_EN-US0765557260_UHD.jpg"},
		{EndDate: "2021-03-081600", CopyRight: "Great blue herons in the Wakodahatchee Wetlands, Delray Beach, Florida (© Marie Hickman/Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.Wakodahatchee_EN-US0593250314_UHD.jpg"},
		{EndDate: "2021-03-071600", CopyRight: "Komodo National Park, Indonesia (© Thrithot/Adobe Stock)", UrlBase: "https://cn.bing.com/th?id=OHR.PadarIsland_EN-US0491336626_UHD.jpg"},
		{EndDate: "2021-03-061600", CopyRight: "Mineral-laden water in the Rio Tinto, Minas de Riotinto mining area, Huelva province, Andalusia, Spain (© David Santiago Garcia/Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.MinasdeRioTinto_EN-US0408244151_UHD.jpg"},
		{EndDate: "2021-03-051600", CopyRight: "Nusa Dua coast with breakwater, Bali, Indonesia (© Dkart/Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.Comma_EN-US0289421685_UHD.jpg"},
		{EndDate: "2021-03-041600", CopyRight: "Female lions in the forest surrounding Lake Nakuru, Kenya (© Scott Davis/Tandem Stills + Motion)", UrlBase: "https://cn.bing.com/th?id=OHR.WWDLions_EN-US0205102042_UHD.jpg"},
		{EndDate: "2021-03-031600", CopyRight: "Volcano Llaima with Araucaria trees in the foreground, Conguillío National Park, Chile (© Fotografías Jorge León Cabello/Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.VolcanoLlaima_EN-US0109967122_UHD.jpg"},
		{EndDate: "2021-02-281600", CopyRight: "Twin polar bear cubs asleep in a snow den in Wapusk National Park, Manitoba, Canada (© AF archive/Alamy)", UrlBase: "https://cn.bing.com/th?id=OHR.TwinsDenning_EN-US9910127756_UHD.jpg"},
		{EndDate: "2021-02-271600", CopyRight: "Red lanterns hanging in Jinli Street, Chengdu, China (© Philippe LEJEANVRE/Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.JinliStreet_EN-US9813774321_UHD.jpg"},
		{EndDate: "2021-02-261600", CopyRight: "Almond blossoms overlooking Trevi, Umbria, Italy (© Maurizio Rellini/eStock Photo)", UrlBase: "https://cn.bing.com/th?id=OHR.Trevi_EN-US7298856463_UHD.jpg"},
		{EndDate: "2021-02-251600", CopyRight: "Le Morne Brabant, Mauritius (© Hemis/Alamy)", UrlBase: "https://cn.bing.com/th?id=OHR.LeMorneBrabant_EN-US7199520186_UHD.jpg"},
		{EndDate: "2021-02-241600", CopyRight: "Dalmatian pelicans on ice, Lake Kerkini, Greece (© Guy Edwardes/Minden Pictures)", UrlBase: "https://cn.bing.com/th?id=OHR.DalmatianPelicans_EN-US7089551223_UHD.jpg"},
		{EndDate: "2021-02-231600", CopyRight: "'Invisible Man,' a memorial to Ralph Ellison in Riverside Park, New York City (© Randy Duchaine/Alamy)", UrlBase: "https://cn.bing.com/th?id=OHR.InvisibleMan_EN-US6967873703_UHD.jpg"},
		{EndDate: "2021-02-221600", CopyRight: "Porto, Portugal (© Kanuman/Shutterstock)", UrlBase: "https://cn.bing.com/th?id=OHR.Porto_EN-US6858177103_UHD.jpg"},
		{EndDate: "2021-02-211600", CopyRight: "Clearing snowstorm, Yosemite National Park, California (© Jeff Lewis/Tandem Stills + Motion)", UrlBase: "https://cn.bing.com/th?id=OHR.AABday_EN-US6703996640_UHD.jpg"},
		{EndDate: "2021-02-201600", CopyRight: "Parrotfish off the coast of Negros Oriental province in the Philippines (© Tim Fitzharris/Minden Pictures)", UrlBase: "https://cn.bing.com/th?id=OHR.Parrotfish_EN-US6474384190_UHD.jpg"},
		{EndDate: "2021-02-191600", CopyRight: "Rocks in the Verzasca River near the hamlet of Lavertezzo in the Valle Verzasca of Switzerland (© Robert Seitz/Offset by Shutterstock)", UrlBase: "https://cn.bing.com/th?id=OHR.VerzascaValley_EN-US6320380092_UHD.jpg"},
		{EndDate: "2021-02-181600", CopyRight: "Perito Moreno Glacier in Patagonia's Los Glaciares National Park, Argentina (© Juergen Schonnop/Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.PeritoMorenoArgentina_EN-US6161367346_UHD.jpg"},
		{EndDate: "2021-02-171600", CopyRight: "Flowers and an ironwork fence in front of a house in New Orleans, Louisiana (© Lauren Mitchell/Offset by Shutterstock)", UrlBase: "https://cn.bing.com/th?id=OHR.PurpleFlowers_EN-US5664268733_UHD.jpg"},
		{EndDate: "2021-02-161600", CopyRight: "Lincoln Memorial in Washington, DC (© White House Photo/Alamy)", UrlBase: "https://cn.bing.com/th?id=OHR.Lincoln50MoWA_EN-US4174714087_UHD.jpg"},
		{EndDate: "2021-02-151600", CopyRight: "Ocean waves crashing over a heart-shaped rock island off the coast of Sydney, Australia (© Kristian Bell/Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.OceanHeart_EN-US5478049854_UHD.jpg"},
		{EndDate: "2021-02-141600", CopyRight: "Eastern bluebirds in Charlotte, North Carolina (© Elizabeth W. Kearley/Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.BluebirdsEastern_EN-US5293227470_UHD.jpg"},
		{EndDate: "2021-02-131600", CopyRight: "Muskox with newborn in the central Arctic coastal plain of Alaska (© Steven Kazlowski/Danita Delimont)", UrlBase: "https://cn.bing.com/th?id=OHR.YearoftheOx_EN-US5106152536_UHD.jpg"},
		{EndDate: "2021-02-121600", CopyRight: "Flowering almond trees in California's Central Valley (© Jeffrey Lewis/Tandem Stills + Motion)", UrlBase: "https://cn.bing.com/th?id=OHR.CentralCaliBlossoms_EN-US0148484264_UHD.jpg"},
		{EndDate: "2021-02-111600", CopyRight: "Nieve penitente ice formations seen on Agua Negra Pass in the Coquimbo Region of the Andes, Chile (© Art Wolfe/Danita Delimont)", UrlBase: "https://cn.bing.com/th?id=OHR.PenitentSnow_EN-US0047515629_UHD.jpg"},
		{EndDate: "2021-02-101600", CopyRight: "Moon dog photographed at Hug Point Falls on the Oregon coast (© Ben Coffman/Tandem Stills + Motion)", UrlBase: "https://cn.bing.com/th?id=OHR.MoonDogs_EN-US0007581724_UHD.jpg"},
		{EndDate: "2021-02-091600", CopyRight: "John Lewis hero mural by Sean Schwab in the Sweet Auburn district of Atlanta, Georgia (© Ilene Perlman/Alamy)", UrlBase: "https://cn.bing.com/th?id=OHR.HeroMural_EN-US9967459324_UHD.jpg"},
		{EndDate: "2021-02-081600", CopyRight: "勃朗峰高山冰川上的徒步者，法国夏慕尼 (© agustavop/Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.IceWalking_ZH-CN5122217505_UHD.jpg"},
		{EndDate: "2021-02-071600", CopyRight: "蒙特利尔的乌林鸮，加拿大 (© rollandgelly/Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.SuperbOwl_ZH-CN5028336455_UHD.jpg"},
		{EndDate: "2021-02-061600", CopyRight: "奥拉基库克山国家公园中的塞夫顿山，新西兰南岛 (© AWL Images/Danita Delimont)", UrlBase: "https://cn.bing.com/th?id=OHR.MountSefton_ZH-CN4956097627_UHD.jpg"},
		{EndDate: "2021-02-051600", CopyRight: "波浪谷中的砂岩层和积水，亚利桑那州朱红悬崖国家纪念碑 (© Dennis Frates/Alamy)", UrlBase: "https://cn.bing.com/th?id=OHR.TheWave_ZH-CN4856809836_UHD.jpg"},
		{EndDate: "2021-02-041600", CopyRight: "北孚日地区自然公园，法国 (© Michel Rauch/Minden Pictures)", UrlBase: "https://cn.bing.com/th?id=OHR.VosgesBioReserve_ZH-CN4762694302_UHD.jpg"},
		{EndDate: "2021-02-031600", CopyRight: "内姆鲁特山上巨大的石灰岩雕像，土耳其阿德亚曼 (© Peerakit JIrachetthakun/Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.MountNemrut_ZH-CN4681788604_UHD.jpg"},
		{EndDate: "2021-02-021600", CopyRight: "大格洛克纳山山峰前的土拨鼠，奥地利 (© SeppFriedhuber/Getty Images)", UrlBase: "https://cn.bing.com/th?id=OHR.RainbowMarmot_ZH-CN4605973404_UHD.jpg"},
		{EndDate: "2021-02-011600", CopyRight: "日落后的托莱多全景，西班牙 (© Frank Fischbach/Alamy)", UrlBase: "https://cn.bing.com/th?id=OHR.ToledoIldefonso_ZH-CN4507206651_UHD.jpg"},
	},
}
