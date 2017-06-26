// Copyright 2016 Platina Systems, Inc. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pci

// Linux PCI code

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

var sysBusPciPath string = "/sys/bus/pci/devices"

func (d *Device) SysfsPath(format string, args ...interface{}) (path string) {
	path = filepath.Join(sysBusPciPath, d.Addr.String(), fmt.Sprintf(format, args...))
	return
}

func (d *Device) SysfsOpenFile(format string, mode int, args ...interface{}) (f *os.File, err error) {
	fn := d.SysfsPath(format, args...)
	f, err = os.OpenFile(fn, mode, 0)
	return
}

func (d *Device) ConfigRw(offset, vʹ, nBytes uint, isWrite bool) (v uint) {
	var f *os.File
	f, err := d.SysfsOpenFile("config", os.O_RDWR)
	defer func() {
		if err != nil {
			panic(err)
		}
	}()
	if err != nil {
		return
	}
	defer f.Close()
	if _, err = f.Seek(int64(offset), os.SEEK_SET); err != nil {
		return
	}
	var b [4]byte
	if isWrite {
		for i := range b {
			b[i] = byte((vʹ >> uint(8*i)) & 0xff)
		}
		_, err = f.Write(b[:nBytes])
		v = vʹ
	} else {
		_, err = f.Read(b[:nBytes])
		if err == nil {
			for i := range b {
				v |= uint(b[i]) << (8 * uint(i))
			}
		}
	}
	return
}

func (d *Device) ReadConfigUint32(o uint) (v uint32) {
	v = uint32(d.BusDevice.ConfigRw(o, 0, 4, false))
	return
}
func (d *Device) WriteConfigUint32(o uint, value uint32) {
	d.BusDevice.ConfigRw(o, uint(value), 4, true)
}
func (d *Device) ReadConfigUint16(o uint) (v uint16) {
	v = uint16(d.BusDevice.ConfigRw(o, 0, 2, false))
	return
}
func (d *Device) WriteConfigUint16(o uint, value uint16) {
	d.BusDevice.ConfigRw(o, uint(value), 2, true)
}
func (d *Device) ReadConfigUint8(o uint) (v uint8) {
	v = uint8(d.BusDevice.ConfigRw(o, 0, 1, false))
	return
}
func (d *Device) WriteConfigUint8(o uint, value uint8) {
	d.BusDevice.ConfigRw(o, uint(value), 1, true)
}

func (d *Device) MapResource(bar uint) (res uintptr, err error) {
	r := &d.Resources[bar]
	var f *os.File
	f, err = d.SysfsOpenFile("resource%d", os.O_RDWR, r.Index)
	if err != nil {
		return
	}
	defer f.Close()
	r.Mem, err = syscall.Mmap(int(f.Fd()), 0, int(r.Size), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		err = fmt.Errorf("mmap resource%d: %s", r.Index, err)
		return
	}
	res = uintptr(unsafe.Pointer(&r.Mem[0]))
	return
}

func (d *Device) UnmapResource(bar uint) (err error) {
	if d.Resources[bar].Mem != nil {
		err = syscall.Munmap(d.Resources[bar].Mem)
		if err != nil {
			return fmt.Errorf("munmap resource%d: %s", bar, err)
		}
	}
	return
}

func DiscoverDevices(bus Bus) (err error) {
	fis, err := ioutil.ReadDir(sysBusPciPath)
	if perr, ok := err.(*os.PathError); ok && perr.Err == syscall.ENOENT {
		return
	}
	if err != nil {
		return
	}
	bc := bus.getBusCommon()
	for _, fi := range fis {
		de := bus.NewDevice()
		d := de.GetDevice()
		d.BusDevice = de

		n := fi.Name()
		if _, err = fmt.Sscanf(n, "%x:%x:%x.%x", &d.Addr.Domain, &d.Addr.Bus, &d.Addr.Slot, &d.Addr.Fn); err != nil {
			return
		}

		var f *os.File
		f, err = d.SysfsOpenFile("config", os.O_RDONLY)
		if err != nil {
			return
		}
		defer f.Close()
		d.configBytes, err = ioutil.ReadAll(f)

		r := bytes.NewReader(d.configBytes)
		binary.Read(r, binary.LittleEndian, &d.Config)
		if d.Config.Type() != Normal {
			continue
		}

		// See if we have a registered driver for this device.
		driver := GetDriver(d.Config.DeviceID)
		if driver == nil {
			continue
		}

		// Loop through BARs to find resources.
		{
			i := 0
			for i < len(d.Config.BaseAddressRegs) {
				bar := d.Config.BaseAddressRegs[i]
				if !bar.Valid() {
					i++
					continue
				}
				var rfi os.FileInfo
				rfi, err = os.Stat(d.SysfsPath("resource%d", i))
				if err != nil {
					return
				}
				r := Resource{
					Index: uint32(i),
					Base:  uint64(bar.Addr()),
					Size:  uint64(rfi.Size()),
				}
				r.BAR[0] = bar

				i++
				is64bit := (bar>>1)&3 == 2
				if is64bit {
					r.BAR[1] = d.Config.BaseAddressRegs[i]
					r.Base |= uint64(r.BAR[1]) << 32
					i++
				}

				d.Resources = append(d.Resources, r)
			}
		}

		d.Driver = driver
		if d.DriverDevice, err = driver.NewDevice(de); err != nil {
			return
		}
		bc.registeredDevs = append(bc.registeredDevs, de)
	}

	// Open all registered devices.
	for _, bd := range bc.registeredDevs {
		if err = bd.Open(); err != nil {
			return
		}
	}
	if err = bus.Validate(); err != nil {
		return
	}

	// Intialize all registered devices that have drivers.
	for _, bd := range bc.registeredDevs {
		d := bd.GetDevice()
		if d.DriverDevice == nil {
			continue
		}
		if err = d.DriverDevice.Init(); err != nil {
			return
		}
	}
	return
}

func CloseDiscoveredDevices(bus Bus) (err error) {
	bc := bus.getBusCommon()
	for _, bd := range bc.registeredDevs {
		d := bd.GetDevice()
		if d.DriverDevice != nil {
			if err = d.DriverDevice.Exit(); err != nil {
				return
			}
		}
		if err = bd.Close(); err != nil {
			return
		}
	}
	return
}
