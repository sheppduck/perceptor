/*
Copyright (C) 2018 Black Duck Software, Inc.

Licensed to the Apache Software Foundation (ASF) under one
or more contributor license agreements. See the NOTICE file
distributed with this work for additional information
regarding copyright ownership. The ASF licenses this file
to you under the Apache License, Version 2.0 (the
"License"); you may not use this file except in compliance
with the License. You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing,
software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
KIND, either express or implied. See the License for the
specific language governing permissions and limitations
under the License.
*/

package core

import (
	"fmt"

	log "github.com/sirupsen/logrus"
)

// Model is the root of the core model
type Model struct {
	// Pods is a map of "<namespace>/<name>" to pod
	Pods                map[string]Pod
	Images              map[DockerImageSha]*ImageInfo
	ImageScanQueue      []Image
	ImageHubCheckQueue  []Image
	ConcurrentScanLimit int
}

func NewModel(concurrentScanLimit int) *Model {
	return &Model{
		Pods:                make(map[string]Pod),
		Images:              make(map[DockerImageSha]*ImageInfo),
		ImageScanQueue:      []Image{},
		ImageHubCheckQueue:  []Image{},
		ConcurrentScanLimit: concurrentScanLimit}
}

// DeletePod removes the record of a pod, but does not affect images.
func (model *Model) DeletePod(podName string) {
	delete(model.Pods, podName)
}

// AddPod adds a pod and all the images in a pod to the model.
// If the pod is already present in the model, it will be removed
// and a new one created in its place.
// The key is the combination of the pod's namespace and name.
// It extract the containers and images from the pod,
// adding them into the cache.
func (model *Model) AddPod(newPod Pod) {
	log.Debugf("about to add pod: UID %s, qualified name %s", newPod.UID, newPod.QualifiedName())
	for _, newCont := range newPod.Containers {
		model.AddImage(newCont.Image)
	}
	log.Debugf("done adding containers+images from pod %s -- %s", newPod.UID, newPod.QualifiedName())
	model.Pods[newPod.QualifiedName()] = newPod
}

// AddImage adds an image to the model, sets its status to NotScanned,
// and adds it to the queue for hub checking.
func (model *Model) AddImage(image Image) {
	_, hasImage := model.Images[image.Sha]
	if !hasImage {
		newInfo := NewImageInfo(image.Sha, image.Name)
		model.Images[image.Sha] = newInfo
		log.Debugf("added image %s to model", image.HumanReadableName())
		model.addImageToHubCheckQueue(image.Sha)
	} else {
		log.Debugf("not adding image %s to model, already have in cache", image.HumanReadableName())
	}
}

// image state transitions

func (model *Model) safeGet(sha DockerImageSha) *ImageInfo {
	results, ok := model.Images[sha]
	if !ok {
		message := fmt.Sprintf("expected to already have image %s, but did not", string(sha))
		log.Error(message)
		panic(message) // TODO get rid of panic
	}
	return results
}

func (model *Model) addImageToHubCheckQueue(sha DockerImageSha) {
	imageInfo := model.safeGet(sha)
	switch imageInfo.ScanStatus {
	case ScanStatusUnknown, ScanStatusError:
		break
	default:
		message := fmt.Sprintf("cannot add image %s to hub check queue, status is neither Unknown nor Error (%s)", sha, imageInfo.ScanStatus)
		log.Error(message)
		panic(message) // TODO get rid of panic
	}
	imageInfo.ScanStatus = ScanStatusInHubCheckQueue
	model.ImageHubCheckQueue = append(model.ImageHubCheckQueue, imageInfo.image())
}

func (model *Model) addImageToScanQueue(sha DockerImageSha) {
	imageInfo := model.safeGet(sha)
	switch imageInfo.ScanStatus {
	case ScanStatusCheckingHub, ScanStatusError:
		break
	default:
		message := fmt.Sprintf("cannot add image %s to scan queue, status is neither CheckingHub nor Error (%s)", sha, imageInfo.ScanStatus)
		log.Error(message)
		panic(message) // TODO get rid of panic
	}
	imageInfo.ScanStatus = ScanStatusInQueue
	model.ImageScanQueue = append(model.ImageScanQueue, imageInfo.image())
}

func (model *Model) getNextImageFromHubCheckQueue() *Image {
	if len(model.ImageHubCheckQueue) == 0 {
		log.Info("hub check queue empty")
		return nil
	}

	first := model.ImageHubCheckQueue[0]
	imageInfo := model.safeGet(first.Sha)
	if imageInfo.ScanStatus != ScanStatusInHubCheckQueue {
		message := fmt.Sprintf("can't start checking hub for image %s, status is not ScanStatusInHubCheckQueue (%s)", string(first.Sha), imageInfo.ScanStatus)
		log.Errorf(message)
		panic(message) // TODO get rid of this panic
	}

	imageInfo.ScanStatus = ScanStatusCheckingHub
	model.ImageHubCheckQueue = model.ImageHubCheckQueue[1:]
	return &first
}

func (model *Model) getNextImageFromScanQueue() *Image {
	if model.inProgressScanCount() >= model.ConcurrentScanLimit {
		log.Infof("max concurrent scan count reached, can't start a new scan -- %v", model.inProgressScanJobs())
		return nil
	}

	if len(model.ImageScanQueue) == 0 {
		log.Info("scan queue empty, can't start a new scan")
		return nil
	}

	first := model.ImageScanQueue[0]
	imageInfo := model.safeGet(first.Sha)
	if imageInfo.ScanStatus != ScanStatusInQueue {
		message := fmt.Sprintf("can't start scanning image %s, status is not InQueue (%s)", string(first.Sha), imageInfo.ScanStatus)
		log.Errorf(message)
		panic(message) // TODO get rid of this panic
	}

	imageInfo.ScanStatus = ScanStatusRunningScanClient
	model.ImageScanQueue = model.ImageScanQueue[1:]
	return &first
}

func (model *Model) errorRunningScanClient(image Image) {
	results := model.safeGet(image.Sha)
	if results.ScanStatus != ScanStatusRunningScanClient {
		message := fmt.Sprintf("cannot error out scan client for image %s, scan client not in progress (%s)", image.HumanReadableName(), results.ScanStatus)
		log.Errorf(message)
		panic(message)
	}
	results.ScanStatus = ScanStatusError
	// TODO get rid of these
	// for now, just readd the image to the queue upon error
	model.addImageToScanQueue(image.Sha)
}

func (model *Model) finishRunningScanClient(image Image) {
	results := model.safeGet(image.Sha)
	if results.ScanStatus != ScanStatusRunningScanClient {
		message := fmt.Sprintf("cannot finish running scan client for image %s, scan client not in progress (%s)", image.HumanReadableName(), results.ScanStatus)
		log.Errorf(message)
		panic(message) // TODO get rid of panic
	}
	results.ScanStatus = ScanStatusRunningHubScan
}

// func (model *Model) finishRunningHubScan(image Image) {
// 	results := model.safeGet(image)
// 	if results.ScanStatus != ScanStatusRunningHubScan {
// 		message := fmt.Sprintf("cannot finish running hub scan for image %s, scan not in progress (%s)", image.HumanReadableName(), results.ScanStatus)
// 		log.Errorf(message)
// 		panic(message)
// 	}
// 	results.ScanStatus = ScanStatusComplete
// }

// additional methods

func (model *Model) inProgressScanJobs() []DockerImageSha {
	inProgressShas := []DockerImageSha{}
	for sha, results := range model.Images {
		switch results.ScanStatus {
		case ScanStatusRunningScanClient, ScanStatusRunningHubScan:
			inProgressShas = append(inProgressShas, sha)
		default:
			break
		}
	}
	return inProgressShas
}

func (model *Model) inProgressScanCount() int {
	return len(model.inProgressScanJobs())
}

func (model *Model) inProgressHubScans() []Image {
	inProgressHubScans := []Image{}
	for _, imageInfo := range model.Images {
		switch imageInfo.ScanStatus {
		case ScanStatusRunningHubScan:
			inProgressHubScans = append(inProgressHubScans, imageInfo.image())
		}
	}
	return inProgressHubScans
}

func (model *Model) scanResults(podName string) (int, int, string, error) {
	pod, ok := model.Pods[podName]
	if !ok {
		return 0, 0, "", fmt.Errorf("could not find pod of name %s in cache", podName)
	}

	overallStatus := ""
	policyViolationCount := 0
	vulnerabilityCount := 0
	for _, container := range pod.Containers {
		imageInfo, ok := model.Images[container.Image.Sha]
		if !ok {
			continue
		}
		if imageInfo.ScanStatus != ScanStatusComplete {
			continue
		}
		if imageInfo.ScanResults == nil {
			continue
		}
		policyViolationCount += imageInfo.ScanResults.PolicyViolationCount()
		vulnerabilityCount += imageInfo.ScanResults.VulnerabilityCount()
		// TODO what's the right way to combine all the 'OverallStatus' values
		//   from the individual image scans?
		if imageInfo.ScanResults.OverallStatus() != "NOT_IN_VIOLATION" {
			overallStatus = imageInfo.ScanResults.OverallStatus()
		}
	}
	return policyViolationCount, vulnerabilityCount, overallStatus, nil
}
