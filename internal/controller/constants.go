package controller

// Common constants shared across all controllers
const (
	// Access controller constants
	AccessTargetsAnnotation          = "maxtac.vtk.io.access/targets"
	AccessDirectionAnnotation        = "maxtac.vtk.io.access/direction"
	AccessOwnerLabel                 = "maxtac.vtk.io.access/owner"
	AccessServiceOwnerNameLabel      = "maxtac.vtk.io.access/serviceOwnerName"
	AccessServiceOwnerNamespaceLabel = "maxtac.vtk.io.access/serviceOwnerNamespace"

	// ClusterAccess controller constants
	ClusterAccessTargetsAnnotation          = "maxtac.vtk.io.clusteraccess/targets"
	ClusterAccessDirectionAnnotation        = "maxtac.vtk.io.clusteraccess/direction"
	ClusterAccessOwnerLabel                 = "maxtac.vtk.io.clusteraccess/owner"
	ClusterAccessServiceOwnerNameLabel      = "maxtac.vtk.io.clusteraccess/serviceOwnerName"
	ClusterAccessServiceOwnerNamespaceLabel = "maxtac.vtk.io.clusteraccess/serviceOwnerNamespace"

	// ExternalAccess controller constants
	ExternalAccessTargetsAnnotation          = "maxtac.vtk.io.externalaccess/targets"
	ExternalAccessDirectionAnnotation        = "maxtac.vtk.io.externalaccess/direction"
	ExternalAccessOwnerLabel                 = "maxtac.vtk.io.externalaccess/owner"
	ExternalAccessServiceOwnerNameLabel      = "maxtac.vtk.io.externalaccess/serviceOwnerName"
	ExternalAccessServiceOwnerNamespaceLabel = "maxtac.vtk.io.externalaccess/serviceOwnerNamespace"

	// ClusterExternalAccess controller constants
	ClusterExternalAccessTargetsAnnotation          = "maxtac.vtk.io.clusterexternalaccess/targets"
	ClusterExternalAccessDirectionAnnotation        = "maxtac.vtk.io.clusterexternalaccess/direction"
	ClusterExternalAccessOwnerLabel                 = "maxtac.vtk.io.clusterexternalaccess/owner"
	ClusterExternalAccessServiceOwnerNameLabel      = "maxtac.vtk.io.clusterexternalaccess/serviceOwnerName"
	ClusterExternalAccessServiceOwnerNamespaceLabel = "maxtac.vtk.io.clusterexternalaccess/serviceOwnerNamespace"
)

// GetAnnotationConfig returns the annotation configuration for the given controller type
func GetAnnotationConfig(controllerType string) AnnotationConfig {
	switch controllerType {
	case "access":
		return AnnotationConfig{
			TargetsAnnotation:          AccessTargetsAnnotation,
			DirectionAnnotation:        AccessDirectionAnnotation,
			OwnerLabel:                 AccessOwnerLabel,
			ServiceOwnerNameLabel:      AccessServiceOwnerNameLabel,
			ServiceOwnerNamespaceLabel: AccessServiceOwnerNamespaceLabel,
		}
	case "clusteraccess":
		return AnnotationConfig{
			TargetsAnnotation:          ClusterAccessTargetsAnnotation,
			DirectionAnnotation:        ClusterAccessDirectionAnnotation,
			OwnerLabel:                 ClusterAccessOwnerLabel,
			ServiceOwnerNameLabel:      ClusterAccessServiceOwnerNameLabel,
			ServiceOwnerNamespaceLabel: ClusterAccessServiceOwnerNamespaceLabel,
		}
	case "externalaccess":
		return AnnotationConfig{
			TargetsAnnotation:          ExternalAccessTargetsAnnotation,
			DirectionAnnotation:        ExternalAccessDirectionAnnotation,
			OwnerLabel:                 ExternalAccessOwnerLabel,
			ServiceOwnerNameLabel:      ExternalAccessServiceOwnerNameLabel,
			ServiceOwnerNamespaceLabel: ExternalAccessServiceOwnerNamespaceLabel,
		}
	case "clusterexternalaccess":
		return AnnotationConfig{
			TargetsAnnotation:          ClusterExternalAccessTargetsAnnotation,
			DirectionAnnotation:        ClusterExternalAccessDirectionAnnotation,
			OwnerLabel:                 ClusterExternalAccessOwnerLabel,
			ServiceOwnerNameLabel:      ClusterExternalAccessServiceOwnerNameLabel,
			ServiceOwnerNamespaceLabel: ClusterExternalAccessServiceOwnerNamespaceLabel,
		}
	default:
		return AnnotationConfig{}
	}
}
