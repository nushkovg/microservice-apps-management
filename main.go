//go:generate goagen bootstrap -d github.com/JormungandrK/microservice-apps-management/design

package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/JormungandrK/microservice-apps-management/app"
	"github.com/JormungandrK/microservice-apps-management/db"
	"github.com/JormungandrK/microservice-security/chain"
	"github.com/JormungandrK/microservice-security/flow"
	"github.com/JormungandrK/microservice-tools/config"
	"github.com/JormungandrK/microservice-tools/gateway"
	"github.com/goadesign/goa"
	"github.com/goadesign/goa/middleware"
)

func main() {
	// Load configuration
	gatewayAdminURL, configFile := loadGatewaySettings()

	conf, err := config.LoadConfig(configFile)
	if err != nil {
		panic(err)
	}
	// Gateway self-registration
	unregisterService := registerMicroservice(gatewayAdminURL, conf)
	defer unregisterService() // defer the unregister for after main exits

	// create security chain
	securityChain, cleanup, err := flow.NewSecurityFromConfig(conf)
	if err != nil {
		panic(err)
	}
	defer cleanup()
	// Create service
	service := goa.New("apps-management")

	// Mount middleware
	service.Use(middleware.RequestID())
	service.Use(middleware.LogRequest(true))
	service.Use(middleware.ErrorHandler(service, true))
	service.Use(middleware.Recover())

	service.Use(chain.AsGoaMiddleware(securityChain))

	dbConf := conf.DBConfig
	// Create new session to MongoDB
	session := db.NewSession(dbConf.Host, dbConf.Username, dbConf.Password, dbConf.DatabaseName)

	// At the end close session
	defer session.Close()

	// Create apps collection and indexes
	index1 := []string{"domain"}
	index2 := []string{"name"}
	indexes := [][]string{index1, index2}
	collectionName := "apps"
	collection := db.PrepareDB(session, dbConf.DatabaseName, collectionName, indexes)

	// Mount "apps" controller
	c := NewAppsController(service, &db.MongoCollection{Collection: collection})
	app.MountAppsController(service, c)
	// Mount "swagger" controller
	c2 := NewSwaggerController(service)
	app.MountSwaggerController(service, c2)

	// Start service
	if err := service.ListenAndServe(fmt.Sprintf(":%d", conf.Service.MicroservicePort)); err != nil {
		service.LogError("startup", "err", err)
	}

}

func loadGatewaySettings() (string, string) {
	gatewayURL := os.Getenv("API_GATEWAY_URL")
	serviceConfigFile := os.Getenv("SERVICE_CONFIG_FILE")

	if gatewayURL == "" {
		gatewayURL = "http://localhost:8001"
	}
	if serviceConfigFile == "" {
		serviceConfigFile = "config.json"
	}

	return gatewayURL, serviceConfigFile
}

func registerMicroservice(gatewayAdminURL string, conf *config.ServiceConfig) func() {
	registration := gateway.NewKongGateway(gatewayAdminURL, &http.Client{}, conf.Service)

	err := registration.SelfRegister()
	if err != nil {
		panic(err)
	}

	return func() {
		registration.Unregister()
	}
}
