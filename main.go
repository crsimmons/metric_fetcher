package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/cloudfoundry-community/go-cfclient"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
)

var api, app_guid, app_route, cf_user, cf_pass, org_name, space_name string

func parseProm(metrics io.ReadCloser) (map[string]*dto.MetricFamily, error) {
	var parser expfmt.TextParser
	mf, err := parser.TextToMetricFamilies(metrics)
	if err != nil {
		return nil, err
	}
	return mf, nil
}

func writeMetrics(mf map[string]*dto.MetricFamily) (bytes.Buffer, error) {
	var buff bytes.Buffer
	for _, v := range mf {
		_, err := expfmt.MetricFamilyToText(&buff, v)
		if err != nil {
			return bytes.Buffer{}, err
		}
	}
	return buff, nil
}

func point(s string) *string {
	return &s
}

func main() {
	api = os.Getenv("API")
	app_guid = os.Getenv("APP_GUID")
	app_route = fmt.Sprintf("https://%s/metrics", os.Getenv("APP_ROUTE"))
	cf_pass = os.Getenv("CF_PASS")
	cf_user = os.Getenv("CF_USER")
	org_name = os.Getenv("ORG_NAME")
	space_name = os.Getenv("SPACE_NAME")
	port, exists := os.LookupEnv("PORT")
	if !exists {
		port = "8080"
	}

	httpClient := &http.Client{}

	c := &cfclient.Config{
		ApiAddress: api,
		Username:   cf_user,
		Password:   cf_pass,
	}
	client, err := cfclient.NewClient(c)
	if err != nil {
		log.Fatalf("failed to initialize cf client: [%q]", err)
	}

	app, err := client.GetAppByGuid(app_guid)
	if err != nil {
		log.Fatalf("failed to get app: [%q]", err)
	}

	req, err := http.NewRequest("GET", app_route, nil)
	if err != nil {
		log.Fatalf("failed to create request: [%q]", err)
	}

	var output bytes.Buffer
	for i := 0; i < app.Instances; i++ {
		req.Header.Add("X-Cf-App-Instance", fmt.Sprintf("%s:%d", app_guid, i))
		resp, err := httpClient.Do(req)
		if err != nil {
			log.Fatalf("failed to get metrics for app %s: [%q]", app.Name, err)
		}
		defer resp.Body.Close()

		mf, err := parseProm(resp.Body)
		if err != nil {
			log.Fatalf("failed to parse prometheus metrics for app %s: [%q]", app.Name, err)
		}

		for _, v := range mf {
			for _, metric := range v.Metric {
				metric.Label = append(metric.Label, &dto.LabelPair{
					Name:  point("org_name"),
					Value: &org_name,
				}, &dto.LabelPair{
					Name:  point("space_name"),
					Value: &space_name,
				}, &dto.LabelPair{
					Name:  point("app_name"),
					Value: &app.Name,
				}, &dto.LabelPair{
					Name:  point("cf_instance_id"),
					Value: point(fmt.Sprintf("%s:%d", app_guid, i)),
				}, &dto.LabelPair{
					Name:  point("cf_instance_number"),
					Value: point(strconv.Itoa(i)),
				})
			}
		}
		metricsBuffer, err := writeMetrics(mf)
		output.Write(metricsBuffer.Bytes())
	}
	http.HandleFunc("/prometheus", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, output.String())
	})
	http.ListenAndServe(fmt.Sprintf(":%s", port), nil)
}
