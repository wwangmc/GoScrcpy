# TYPE=0: rootfull, TYPE=1: rootless, TYPE=2: RootHide
TYPE ?= 1
DEBUG = 0
FINALPACKAGE = 1

ifeq ($(TYPE),0)
ARCHS ?= arm64
TARGET = iphone:clang:16.5:13.0
THEOS_PACKAGE_SCHEME =
else ifeq ($(TYPE),1)
ARCHS ?= arm64 arm64e
TARGET = iphone:clang:16.5:14.0
THEOS_PACKAGE_SCHEME = rootless
else ifeq ($(TYPE),2)
ARCHS ?= arm64 arm64e
TARGET = iphone:clang:16.5:14.0
THEOS_PACKAGE_SCHEME = roothide
else
$(error Unsupported TYPE '$(TYPE)'; use 0, 1 or 2)
endif

# Reopen the selected application; installing does not respring the phone.

include $(THEOS)/makefiles/common.mk

TWEAK_NAME = AFProbe
AFProbe_FILES = Sources/Probe.m Shared/AFState.m Sources/AFRebind.c
AFProbe_CFLAGS = -I$(THEOS_PROJECT_DIR)/Shared
AFProbe_OBJCFLAGS = -fobjc-arc
AFProbe_FRAMEWORKS = Foundation

include $(THEOS_MAKE_PATH)/tweak.mk

TOOL_NAME = gosc-afprobe
gosc-afprobe_FILES = afctl/main.m Shared/AFState.m
gosc-afprobe_CFLAGS = -I$(THEOS_PROJECT_DIR)/Shared
gosc-afprobe_OBJCFLAGS = -fobjc-arc
gosc-afprobe_FRAMEWORKS = Foundation
gosc-afprobe_CODESIGN_FLAGS = -SEntitlements.plist
gosc-afprobe_INSTALL_PATH = /usr/bin
include $(THEOS_MAKE_PATH)/tool.mk
