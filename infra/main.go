package main

import (
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/cloudrun"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		conf := config.New(ctx, "")
		project := conf.Require("project")
		region := conf.Require("region")

		serviceName := "scavenger"

		image := "sthanguy/scavenger-server:latest"

		_, err := cloudrun.NewService(ctx, serviceName, &cloudrun.ServiceArgs{
			Name:     pulumi.String(serviceName),
			Project:  pulumi.String(project),
			Location: pulumi.String(region),
			Template: &cloudrun.ServiceTemplateArgs{
				Spec: &cloudrun.ServiceTemplateSpecArgs{
					Containers: cloudrun.ServiceTemplateSpecContainerArray{
						&cloudrun.ServiceTemplateSpecContainerArgs{
							Image: pulumi.String(image),
							Ports: &cloudrun.ServiceTemplateSpecContainerPortArray{
								&cloudrun.ServiceTemplateSpecContainerPortArgs{
									ContainerPort: pulumi.Int(3000),
								},
							},
							StartupProbe: &cloudrun.ServiceTemplateSpecContainerStartupProbeArgs{
								HttpGet: &cloudrun.ServiceTemplateSpecContainerStartupProbeHttpGetArgs{
									Port: pulumi.Int(3000),
									Path: pulumi.String("/healthz"),
								},
								InitialDelaySeconds: pulumi.Int(20),
								FailureThreshold:    pulumi.Int(10),
								PeriodSeconds:       pulumi.Int(10),
							},
						},
					},
				},
			},
			Traffics: cloudrun.ServiceTrafficArray{
				&cloudrun.ServiceTrafficArgs{
					Percent:        pulumi.Int(100),
					LatestRevision: pulumi.Bool(true),
				},
			},
		})
		if err != nil {
			return err
		}

		return nil
	})
}
