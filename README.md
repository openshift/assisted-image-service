# Assisted Image Service

This service provides a separate implementation of managing assisted service rhcos images.
It downloads a set of rhcos images on startup based on config and responds to a single API endpoint to allow a user to download an image for a given assisted-service cluster.
