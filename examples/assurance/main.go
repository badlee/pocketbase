package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/plugins/ghupdate"
	"github.com/pocketbase/pocketbase/plugins/jsvm"
	"github.com/pocketbase/pocketbase/plugins/luavm"
	"github.com/pocketbase/pocketbase/plugins/migratecmd"
	"github.com/pocketbase/pocketbase/plugins/wasm"
	flag "github.com/spf13/pflag"
)

func main() {
	flags := flag.FlagSet{}

	flags.StringP(
		"dir-data",
		"d",
		"./database",
		"Le répertoire contenant les données de l'application",
	)
	flags.StringP(
		"encryption-env",
		"e",
		"",
		"la nom de la variable ENV dont la valeur de 32 caractères sera utilisée\ncomme clé de chiffrement pour les paramètres de l'application (aucune par défaut)",
	)
	flags.StringP(
		"trace",
		"D",
		"",
		"Afficher des journaux et des instructions SQL sur la console",
	)
	flags.StringSlice(
		"serve",
		[]string{},
		"Démarre le server web (par defaut sur 127.0.0.1:8090 si auccun domain n'as ete specifié)",
	)
	flags.StringSlice(
		"serve-origins",
		[]string{},
		"Liste des origines de domaine autorisées CORS",
	)
	flags.String(
		"serve-http",
		"",
		"Adresse TCP à écouter pour le serveur HTTP\n(si les arguments de domaine sont spécifiés - par défaut à 0.0.0.0:80, sinon 127.0.0.1:8090)",
	)
	flags.String(
		"serve-https",
		"",
		"Adresse TCP pour écouter le serveur HTTPS\n(si les arguments de domaine sont spécifiés - par défaut 0.0.0.0:443, sinon - par défaut une chaîne vide, c'est-à-dire pas de TLS)\nLe trafic HTTP entrant sera également automatiquement redirigé vers HTTPS version",
	)
	flags.String(
		"serve-socket-io-path",
		"/socket.io",
		"Définit le chemin sous lequel socket.io et les fichiers statiques seront servis. La valeur par défaut est /socket.io",
	)
	flags.String(
		"admin",
		"",
		"Gestion des comptes administrateurs",
	)
	flags.String(
		"admin-create",
		"",
		"Crée un nouveau compte administrateur",
	)
	flags.String(
		"admin-delete",
		"",
		"Supprime un compte administrateur existant",
	)
	flags.String(
		"admin-list",
		"",
		"Répertorier tous les comptes d'administrateur existants",
	)
	flags.String(
		"admin-update",
		"",
		"Modifie le mot de passe d'un seul compte administrateur",
	)
	app := pocketbase.NewWithConfig(pocketbase.Config{
		DefaultDataDir:  "./database",
		HideStartBanner: false,
		DefaultDev:      false,
		DefaultAppName:  "Assurances",
		Flags:           &flags,
	})
	app.RootCmd.Short = app.Settings().Meta.AppName

	// ---------------------------------------------------------------
	// Optional plugin flags:
	// ---------------------------------------------------------------

	var hooksDir string
	app.RootCmd.PersistentFlags().StringVar(
		&hooksDir,
		"dir-scripts",
		"./scripts",
		"Le répertoire contenant les scripts de l'application",
	)

	var hooksWatch bool
	app.RootCmd.PersistentFlags().BoolVar(
		&hooksWatch,
		"script-watch",
		true,
		"Redémarrez automatiquement l'application lors de la modification d'un script",
	)

	var hooksPool int
	app.RootCmd.PersistentFlags().IntVar(
		&hooksPool,
		"script-pool",
		25,
		"Le nombre total d'instances pour l'exécution des script",
	)

	var migrationsDir string
	app.RootCmd.PersistentFlags().StringVar(
		&migrationsDir,
		"dir-migrations",
		"./migrations",
		"Chemin du répertoire de migrations",
	)

	var automigrate bool
	app.RootCmd.PersistentFlags().BoolVar(
		&automigrate,
		"automigrate",
		true,
		"Activer/Desactiver les migrations automatique",
	)

	var publicDir string
	app.RootCmd.PersistentFlags().StringVar(
		&publicDir,
		"dir-public",
		"./public",
		"Chemin du répertoire de fichier publique",
	)

	var repoGit string
	app.RootCmd.PersistentFlags().StringVar(
		&repoGit,
		"github",
		"pocketbase/pocketbase",
		"nom du repertoire github.com (par défaut : pocketbase/pocketbase)",
	)

	var indexFallback bool
	app.RootCmd.PersistentFlags().BoolVar(
		&indexFallback,
		"index-fallback",
		true,
		"Fichier pr defaut quand index.html n'existe pas",
	)

	var queryTimeout int
	app.RootCmd.PersistentFlags().IntVar(
		&queryTimeout,
		"select-timeout",
		30,
		"Le délai maximum d'execution des requêtes SELECT (en secondes)",
	)

	app.RootCmd.ParseFlags(os.Args[1:])

	// ---------------------------------------------------------------
	// Plugins and hooks:
	// ---------------------------------------------------------------

	// load jsvm (hooks and migrations)
	jsvm.MustRegister(app, jsvm.Config{
		MigrationsDir:     migrationsDir,
		HooksDir:          hooksDir,
		HooksFilesPattern: `^.*(\.js|\.ts)$`,
		HooksWatch:        hooksWatch,
		HooksWatchPattern: `^.*([^\.cmd]\.js|\.ts)$`,
		HooksPoolSize:     hooksPool,
	})

	// load jsvm (hooks and migrations)
	luavm.MustRegister(app, luavm.Config{
		MigrationsDir:     migrationsDir,
		HooksDir:          hooksDir,
		HooksFilesPattern: `^.*(\.lua|\.luac)$`,
		HooksWatch:        hooksWatch,
		HooksPoolSize:     hooksPool,
	})

	wasm.MustRegister(app, wasm.Config{
		MigrationsDir:     migrationsDir,
		HooksDir:          hooksDir,
		HooksFilesPattern: `^.*\.wasm$`,
		HooksWatch:        hooksWatch,
		HooksPoolSize:     hooksPool,
	})

	// migrate command (with js templates)
	migratecmd.MustRegister(app, app.RootCmd, migratecmd.Config{
		TemplateLang: migratecmd.TemplateLangJS,
		Automigrate:  automigrate,
		Dir:          migrationsDir,
	})

	// GitHub selfupdate
	rep := strings.Split(repoGit, "/")
	gitOwner := "badlee"
	gitRepo := "pocketbase"
	if len(rep) == 1 {
		gitRepo = rep[0]
		gitOwner = rep[0]
	} else if len(rep) >= 2 {
		gitRepo = rep[0]
		gitOwner = rep[1]
	}
	ghupdate.MustRegister(app, app.RootCmd, ghupdate.Config{
		Owner: gitOwner,
		Repo:  gitRepo,
	})

	app.OnAfterBootstrap().PreAdd(func(e *core.BootstrapEvent) error {
		app.Dao().ModelQueryTimeout = time.Duration(queryTimeout) * time.Second
		return nil
	})

	app.OnBeforeServe().Add(func(e *core.ServeEvent) error {
		// serves static files from the provided public dir (if exists)
		e.Router.GET("/*", apis.StaticDirectoryHandler(os.DirFS(publicDir), indexFallback))
		return nil
	})

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}

// the default public dir location is relative to the executable
type FileType int

// String - Creating common behavior - give the type a String function
func (w FileType) String() string {
	return [...]string{"Not Found", "File", "Directory"}[w-1]
}

// EnumIndex - Creating common behavior - give the type a EnumIndex function
func (w FileType) Index() int {
	return int(w)
}

// EnumIndex - Creating common behavior - give the type a EnumIndex function
func (w FileType) Name() string {
	return w.String()
}

const (
	IS_NOT_FOUND FileType = iota
	IS_FILE
	IS_DIR
)

func exists(path string) (FileType, error) {
	dirOrFile, err := os.Stat(path)
	if err == nil {
		if dirOrFile.IsDir() {
			return IS_DIR, nil
		} else {
			return IS_FILE, nil
		}
	}
	if os.IsNotExist(err) {
		return IS_NOT_FOUND, err
	}
	if dirOrFile.IsDir() {
		return IS_DIR, err
	} else {
		return IS_FILE, err
	}
}

func getDir(directory string) string {
	var pwd = filepath.Join(os.Args[0], "../")
	if strings.HasPrefix(directory, "../") {
		pwd = filepath.Dir(filepath.Join(os.Args[0], directory))
	}
	if !filepath.IsAbs(directory) {
		if _pwd, err := os.Getwd(); err == nil {
			pwd = _pwd
		}
	} else {
		pwd = filepath.Join(directory, "../")
		directory = filepath.Base(directory)
		if _, err := filepath.Abs(pwd); err != nil || pwd == directory {
			if err != nil {
				return directory
			}
		}
	}

	if typePwd, err := exists(pwd); err != nil || typePwd != IS_DIR {
		// if directory is not valid
		pwd = "."
	}

	if _pwd, err := filepath.Abs(pwd); err == nil {
		pwd = _pwd
	}
	if strings.HasPrefix(pwd, os.TempDir()) {
		// most likely ran with go run
		return fmt.Sprintf("./%s", directory)
	}

	return filepath.Join(pwd, directory)
}
