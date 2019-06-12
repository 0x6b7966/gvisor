// Copyright 2019 The gVisor Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ext4

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"gvisor.googlesource.com/gvisor/pkg/abi/linux"
	"gvisor.googlesource.com/gvisor/pkg/sentry/context"
	"gvisor.googlesource.com/gvisor/pkg/sentry/context/contexttest"
	"gvisor.googlesource.com/gvisor/pkg/sentry/fs"
	"gvisor.googlesource.com/gvisor/pkg/syserror"
	"gvisor.googlesource.com/gvisor/runsc/test/testutil"
)

const (
	tinyImagePath = "pkg/sentry/fs/ext4/assets/tiny.ext4"
)

// createOptions creates the options string which is passed into Mount() as `data`.
func createOptions(fd uintptr) string {
	return fmt.Sprintf("%v=%d", fdKey, fd)
}

// setUp creates a new MountNamespace from the given ext4 image. If err is non-nil,
// then it also returns a tearDown function which must be called at the end of the test.
func setUp(t *testing.T) (context.Context, *fs.MountNamespace, func() error, error) {
	imagePath, err := testutil.FindFile(tinyImagePath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("testutil.FindFile: %v", err)
	}

	f, err := os.Open(imagePath)
	if err != nil {
		return nil, nil, nil, err
	}

	// Mount the ext4 fs and retrieve the Inode structure for the file.
	ext4fs := filesystem{}
	mockCtx := contexttest.Context(t)
	rootInode, err := ext4fs.Mount(mockCtx, imagePath, fs.MountSourceFlags{ReadOnly: true}, createOptions(f.Fd()), nil)
	if err != nil {
		f.Close()
		return nil, nil, nil, err
	}

	mns, err := fs.NewMountNamespace(mockCtx, rootInode)
	if err != nil {
		f.Close()
		return nil, nil, nil, err
	}

	tearDown := f.Close
	return mockCtx, mns, tearDown, nil
}

// TestReadlink tests Readlink functionality.
func TestReadlink(t *testing.T) {
	type readlinkTest struct {
		name     string
		filePath string
		wantErr  error
		wantLink string
	}

	tests := []readlinkTest{
		{
			name:     "Readlink on a symlink",
			filePath: "/symlink.txt",
			wantErr:  nil,
			wantLink: "file.txt",
		},
		{
			name:     "Readlink on a regular file",
			filePath: "/file.txt",
			wantErr:  syserror.ENOLINK,
			wantLink: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, mns, tearDown, err := setUp(t)
			if err != nil {
				t.Fatalf("setUp failed: %v", err)
			}

			defer func() {
				if err := tearDown(); err != nil {
					t.Fatalf("tearDown failed: %v", err)
				}
			}()

			// Traverse to the parent inode of the file, then call lookup and then call
			// GetLink on the resultant inode. We do this because calling FindInode on
			// the filePath directly would resolve the symlink preventing us from
			// getting a symlink inode.
			dir, file := filepath.Split(test.filePath)

			// filePath should not be pointing to root dir.
			if file == "" {
				t.Fatalf("Bad testcase. filePath points to root directory.")
			}

			remainingTraversals := uint(0)
			dirent, err := mns.FindInode(ctx, mns.Root(), nil, dir, &remainingTraversals)

			// We must find the inode so check for err.
			if err != nil || dirent == nil || dirent.IsNegative() {
				t.Fatalf("MountNamespace.FindInode failed: Got:\n\t dirent: %v\n\terr:%v", dirent, err)
			}

			defer dirent.DecRef()

			fileDirent, err := dirent.Inode.Lookup(ctx, file)

			// We must find the inode so check for err.
			if err != nil || fileDirent == nil || fileDirent.IsNegative() {
				t.Fatalf("Inode.Lookup failed: Got:\n\t dirent: %v\n\terr:%v", fileDirent, err)
			}

			defer fileDirent.DecRef()

			linkPath, err := fileDirent.Inode.Readlink(ctx)

			if diff := cmp.Diff(test.wantLink, linkPath); diff != "" {
				t.Errorf("inode.Readlink() link mismatch (-want +got):\n%s", diff)
			}

			if test.wantErr != err {
				t.Errorf("inode.Readlink() error mismatch: \n\t wanted %v \n\t got %v", test.wantErr, err)
			}
		})
	}
}

// TestGetlink tests Getlink functionality.
func TestGetlink(t *testing.T) {
	type getlinkTest struct {
		name     string
		filePath string
		want     error
	}

	tests := []getlinkTest{
		{
			name:     "Getlink on a symlink",
			filePath: "/symlink.txt",
			want:     fs.ErrResolveViaReadlink,
		},
		{
			name:     "Getlink on a regular file",
			filePath: "/file.txt",
			want:     syserror.ENOLINK,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, mns, tearDown, err := setUp(t)
			if err != nil {
				t.Fatalf("setUp failed: %v", err)
			}

			defer func() {
				if err := tearDown(); err != nil {
					t.Fatalf("tearDown failed: %v", err)
				}
			}()

			// Traverse to the parent inode of the file, then call lookup and then call
			// GetLink on the resultant inode. We do this because calling FindInode on
			// the filePath directly would resolve the symlink preventing us from
			// getting a symlink inode.
			dir, file := filepath.Split(test.filePath)

			// filePath should not be pointing to root dir.
			if file == "" {
				t.Fatalf("Bad testcase. filePath points to root directory.")
			}

			remainingTraversals := uint(0)
			dirent, err := mns.FindInode(ctx, mns.Root(), nil, dir, &remainingTraversals)

			// We must find the inode so check for err.
			if err != nil || dirent == nil || dirent.IsNegative() {
				t.Fatalf("MountNamespace.FindInode failed: Got:\n\t dirent: %v\n\terr:%v", dirent, err)
			}

			defer dirent.DecRef()

			fileDirent, err := dirent.Inode.Lookup(ctx, file)

			// We must find the inode so check for err.
			if err != nil || fileDirent == nil || fileDirent.IsNegative() {
				t.Fatalf("Inode.Lookup failed: Got:\n\t dirent: %v\n\terr:%v", fileDirent, err)
			}

			defer fileDirent.DecRef()

			getlinkDirent, err := fileDirent.Inode.Getlink(ctx)
			if getlinkDirent != nil {
				t.Fatalf("Getlink returned a non-nil dirent: %v", getlinkDirent)
			}

			if test.want != err {
				t.Errorf("inode.Getlink() error mismatch: \n\t wanted %v \n\t got %v", test.want, err)
			}
		})
	}
}

// TestLookup tests the lookup functionality.
func TestLookup(t *testing.T) {
	type lookupTest struct {
		name     string
		filePath string
		expected bool
	}

	tests := []lookupTest{
		{
			name:     "Lookup existing file",
			filePath: "/file.txt",
			expected: true,
		},
		{
			name:     "Lookup non existing file",
			filePath: "/non-existent-file.txt",
			expected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, mns, tearDown, err := setUp(t)
			if err != nil {
				t.Fatalf("setUp failed: %v", err)
			}

			defer func() {
				if err := tearDown(); err != nil {
					t.Fatalf("tearDown failed: %v", err)
				}
			}()

			remainingTraversals := uint(0)
			dirent, err := mns.FindInode(ctx, mns.Root(), nil, test.filePath, &remainingTraversals)
			if dirent != nil {
				defer dirent.DecRef()
			}

			if test.expected {
				// Should be a non-nil Dirent containing a non-nil Inode, and a nil
				// error.
				if err != nil || dirent == nil || dirent.IsNegative() {
					t.Errorf("Lookup failed to find a file which exists: %v", err)
				}
			} else {
				switch err {
				case syserror.ENOENT:
					// Should be nil Dirent and a non-nil error (syserror.ENOENT).
					if dirent != nil {
						t.Error("Lookup returned non-nil dirent and ENOENT for non-existing file. Expected nil dirent.")
					}
				case nil:
					// Should be a non-nil Dirent containing a nil Inode and a nil error.
					if dirent == nil || !dirent.IsNegative() {
						t.Errorf("Expected negative dirent for non-existent file since error is nil. Got %v", dirent)
					}
				default:
					t.Errorf("Lookup returned a valid dirent for a non-existent file. Got:\n\t dirent: %v\n\terr:%v", dirent, err)
				}
			}
		})
	}
}

// TestUnstableAttr tests the unstable attributes of inodes. (Depends on lookup
// for finding the inode.)
func TestUnstableAttr(t *testing.T) {
	type unstableAttrTest struct {
		name string
		want fs.UnstableAttr
		// Path to the file in the ext4 image being tested. This should be an
		// absolute path wrt the root of the ext4 fs.
		filePath string
	}

	tests := []unstableAttrTest{
		{
			name: "root directory unstable attr",
			want: fs.UnstableAttr{
				Size:  1024,
				Perms: fs.FilePermsFromMode(linux.FileMode(0755)),
				Owner: fs.FileOwner{
					UID: 0,
					GID: 0,
				},
				Links: 3,
			},
			filePath: "/",
		},
		{
			name: "regular file unstable attr",
			want: fs.UnstableAttr{
				Size:  int64(len("Hello World!")) + 1, // Include null byte.
				Perms: fs.FilePermsFromMode(linux.FileMode(0644)),
				Owner: fs.FileOwner{
					UID: 0,
					GID: 0,
				},
				Links: 1,
			},
			filePath: "/file.txt",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, mns, tearDown, err := setUp(t)
			if err != nil {
				t.Fatalf("setUp failed: %v", err)
			}

			defer func() {
				if err := tearDown(); err != nil {
					t.Fatalf("tearDown failed: %v", err)
				}
			}()

			remainingTraversals := uint(0)
			dirent, err := mns.FindInode(ctx, mns.Root(), nil, test.filePath, &remainingTraversals)
			if dirent != nil {
				defer dirent.DecRef()
			}

			if err != nil {
				t.Fatalf("mountNamespace.FindInode could not find inode: %v", err)
			}

			unstableAttr, err := dirent.Inode.UnstableAttr(ctx)
			if err != nil {
				t.Fatalf("inode.UnstableAttr: %v", err)
			}

			// Ignore the time based attributes for now because these are subject to
			// change as the underlying image changes.
			cmpIgnoreTimeFields := cmp.FilterPath(func(p cmp.Path) bool {
				return p.String() == "AccessTime" || p.String() == "ModificationTime" || p.String() == "StatusChangeTime"
			}, cmp.Ignore())

			// Check if the unstable attributes are as expected, else report the diff.
			if diff := cmp.Diff(test.want, unstableAttr, cmpIgnoreTimeFields); diff != "" {
				t.Errorf("inode.UnstableAttr() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
