package core

import (
	"fmt"

	common "bitbucket.org/bdsengineering/perceptor/pkg/common"
	log "github.com/sirupsen/logrus"
)

// Model is the root of the core model
type Model struct {
	// map of "<namespace>/<name>" to pod
	Pods                map[string]common.Pod
	Images              map[common.Image]*ImageScanResults
	ImageScanQueue      []common.Image
	ConcurrentScanLimit int
}

func NewModel(concurrentScanLimit int) *Model {
	return &Model{
		Pods:                make(map[string]common.Pod),
		Images:              make(map[common.Image]*ImageScanResults),
		ImageScanQueue:      []common.Image{},
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
func (model *Model) AddPod(newPod common.Pod) {
	log.Infof("about to add pod: UID %s, qualified name %s", newPod.UID, newPod.QualifiedName())
	for _, newCont := range newPod.Containers {
		model.AddImage(newCont.Image)
	}
	log.Infof("done adding containers+images from pod %s -- %s", newPod.UID, newPod.QualifiedName())
	model.Pods[newPod.QualifiedName()] = newPod
}

// AddImage adds an image to the cache, and
// queues it up for scanning if it hasn't yet been seen.
func (model *Model) AddImage(image common.Image) {
	_, hasImage := model.Images[image]
	if !hasImage {
		addedImage := NewImageScanResults()
		model.Images[image] = addedImage
		log.Infof("adding image %s to image scan queue", image.Name())
		model.addImageToQueue(image)
	} else {
		log.Infof("not adding image %s to image scan queue, already have in cache", image.Name())
	}
}

// image state transitions

func (model *Model) safeGet(image common.Image) *ImageScanResults {
	results, ok := model.Images[image]
	if !ok {
		message := fmt.Sprintf("expected to already have image %s, but did not", image.Name())
		log.Error(message)
		panic(message)
	}
	return results
}

func (model *Model) addImageToQueue(image common.Image) {
	results := model.safeGet(image)
	switch results.ScanStatus {
	case ScanStatusNotScanned, ScanStatusError:
		break
	default:
		message := fmt.Sprintf("cannot add image %s to queue, status is neither NotScanned nor Error (%d)", image.Name(), results.ScanStatus)
		log.Error(message)
		panic(message)
	}
	results.ScanStatus = ScanStatusInQueue
	model.ImageScanQueue = append(model.ImageScanQueue, image)
}

func (model *Model) getNextImageFromQueue() *common.Image {
	if len(model.ImageScanQueue) == 0 {
		return nil
	}

	first := model.ImageScanQueue[0]
	results := model.safeGet(first)
	if results.ScanStatus != ScanStatusInQueue {
		message := fmt.Sprintf("can not start scanning image %s, status is not InQueue (%d)", first.Name(), results.ScanStatus)
		log.Errorf(message)
		panic(message)
	}

	results.ScanStatus = ScanStatusRunningScanClient
	model.ImageScanQueue = model.ImageScanQueue[1:]
	return &first
}

func (model *Model) errorRunningScanClient(image common.Image) {
	results := model.safeGet(image)
	if results.ScanStatus != ScanStatusRunningScanClient {
		message := fmt.Sprintf("can not error out scan client for image %s, scan client not in progress (%d)", image.Name(), results.ScanStatus)
		log.Errorf(message)
		panic(message)
	}
	results.ScanStatus = ScanStatusError
	// for now, just readd the image to the queue upon error
	model.addImageToQueue(image)
}

func (model *Model) finishRunningScanClient(image common.Image) {
	results := model.safeGet(image)
	if results.ScanStatus != ScanStatusRunningScanClient {
		message := fmt.Sprintf("can not finish running scan client for image %s, scan client not in progress (%d)", image.Name(), results.ScanStatus)
		log.Errorf(message)
		panic(message)
	}
	results.ScanStatus = ScanStatusRunningHubScan
}

// func (model *Model) finishRunningHubScan(image common.Image) {
// 	results := model.safeGet(image)
// 	if results.ScanStatus != ScanStatusRunningHubScan {
// 		message := fmt.Sprintf("can not finish running hub scan for image %s, scan not in progress (%d)", image.Name(), results.ScanStatus)
// 		log.Errorf(message)
// 		panic(message)
// 	}
// 	results.ScanStatus = ScanStatusComplete
// }

// additional methods

func (model *Model) inProgressScanJobs() []common.Image {
	inProgressImages := []common.Image{}
	for image, results := range model.Images {
		switch results.ScanStatus {
		case ScanStatusRunningScanClient, ScanStatusRunningHubScan:
			inProgressImages = append(inProgressImages, image)
		default:
			break
		}
	}
	return inProgressImages
}

func (model *Model) inProgressScanCount() int {
	return len(model.inProgressScanJobs())
}

func (model *Model) scanResults(podName string) (*ScanResults, error) {
	pod, ok := model.Pods[podName]
	if !ok {
		return nil, fmt.Errorf("could not find pod of name %s in cache", podName)
	}

	overallStatus := ""
	policyViolationCount := 0
	vulnerabilityCount := 0
	for _, container := range pod.Containers {
		imageScanResults, ok := model.Images[container.Image]
		if !ok {
			continue
		}
		if imageScanResults.ScanStatus != ScanStatusComplete {
			continue
		}
		if imageScanResults.ScanResults == nil {
			continue
		}
		policyViolationCount += imageScanResults.ScanResults.PolicyViolationCount
		vulnerabilityCount += imageScanResults.ScanResults.VulnerabilityCount
		// TODO what's the right way to combine all the 'OverallStatus' values
		//   from the individual image scans?
		if imageScanResults.ScanResults.OverallStatus != "NOT_IN_VIOLATION" {
			overallStatus = imageScanResults.ScanResults.OverallStatus
		}
	}
	return &ScanResults{
		OverallStatus:        overallStatus,
		PolicyViolationCount: policyViolationCount,
		VulnerabilityCount:   vulnerabilityCount,
	}, nil
}
