/*
Copyright 2018 Iguazio Systems Ltd.

Licensed under the Apache License, Version 2.0 (the "License") with
an addition restriction as set forth herein. You may not use this
file except in compliance with the License. You may obtain a copy of
the License at http://www.apache.org/licenses/LICENSE-2.0.

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
implied. See the License for the specific language governing
permissions and limitations under the License.

In addition, you may not use the software for any purposes that are
illegal under applicable law, and the grant of the foregoing license
under the Apache 2.0 license is conditioned upon your compliance with
such restriction.
*/

package tsdb

import (
	"github.com/pkg/errors"
	"github.com/v3io/frames"
	"github.com/v3io/v3io-tsdb/pkg/querier"
	tsdbutils "github.com/v3io/v3io-tsdb/pkg/utils"
	"time"
)

type tsdbIterator struct {
	request     *frames.ReadRequest
	set         querier.SeriesSet
	err         error
	withColumns bool
	currFrame   frames.Frame
}

func (b *Backend) Read(request *frames.ReadRequest) (frames.FrameIterator, error) {

	step, err := tsdbutils.Str2duration(request.StepRaw)
	if err != nil {
		return nil, err
	}

	// TODO: start & end times
	to := time.Now().Unix() * 1000
	if request.To != "" {
		to, err = tsdbutils.Str2unixTime(request.To)
		if err != nil {
			return nil, err
		}
	}

	from := to - 1000*3600 // default of last hour
	if request.From != "" {
		from, err = tsdbutils.Str2unixTime(request.From)
		if err != nil {
			return nil, err
		}
	}

	b.logger.DebugWith("Query", "from", from, "to", to, "table", request.Table,
		"filter", request.Filter, "functions", request.Aggragators, "step", step)

	adapter, err := b.GetAdapter(request.Table)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create adapter")
	}

	qry, err := adapter.Querier(nil, from, to)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to initialize Querier")
	}

	iter := tsdbIterator{request: request}
	name := ""
	if len(request.Columns) > 0 {
		name = request.Columns[0]
		iter.withColumns = true
	}

	iter.set, err = qry.Select(name, request.Aggragators, step, request.Filter)
	if err != nil {
		return nil, errors.Wrap(err, "Failed on TSDB Select")
	}

	return &iter, nil
}

func (i *tsdbIterator) Next() bool {

	if i.set.Next() {
		series := i.set.At()
		labels := map[string]interface{}{}
		values := []float64{}
		times := []time.Time{}

		for _, v := range series.Labels() {
			labels[v.Name] = v.Value
		}

		iter := series.Iterator()
		for iter.Next() {
			t, v := iter.At()
			values = append(values, v)
			times = append(times, time.Unix(t/1000, (t%1000)*1000))
		}

		if iter.Err() != nil {
			i.err = iter.Err()
			return false
		}

		timeCol, err := frames.NewSliceColumn("Date", times)
		if err != nil {
			i.err = err
			return false
		}

		colname := "values"
		if i.withColumns {
			colname = i.request.Columns[0]
		}
		valCol, err := frames.NewSliceColumn(colname, values)
		if err != nil {
			i.err = err
			return false
		}

		columns := []frames.Column{timeCol, valCol}
		i.currFrame, err = frames.NewFrame(columns, "Date", labels)
		if err != nil {
			i.err = err
			return false
		}

		return true
	}

	if i.set.Err() != nil {
		i.err = i.set.Err()
	}

	return false
}

func (i *tsdbIterator) Err() error {
	return i.err
}

func (i *tsdbIterator) At() frames.Frame {
	return i.currFrame
}