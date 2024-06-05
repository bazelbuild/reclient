// Copyright 2024 Google LLC
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
import { Injectable } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { CollectionViewer, DataSource } from "@angular/cdk/collections";
import { BehaviorSubject, Observable, combineLatest } from 'rxjs';
import { catchError, finalize, of, map } from 'rxjs';

export interface LogRecord {
  command: Command
  result: Result
  remoteMetadata: RemoteMetadata
  localMetadata: LocalMetadata
  completionStatus: string
  index: number
}

export interface Command {
  identifiers: Identifiers
  execRoot: string
  output: Output
  executionTimeout: number
  workingDirectory: string
  platform: Platform
}

export interface Identifiers {
  commandId: string
  invocationId: string
  toolName: string
  executionId: string
}

export interface Output { }

export interface Platform {
  OSFamily?: string
  Pool?: string
  "container-image"?: string
}

export interface Result {
  status: string
  exitCode: number
  msg: string
}

export interface RemoteMetadata {
  result: Result2
  numInputDirectories: number
  totalInputBytes: string
  commandDigest: string
  actionDigest: string
  eventTimes: EventTimes
  logicalBytesUploaded: string
  realBytesUploaded: string
  stderrDigest: string
  stdoutDigest: string
}

export interface Result2 {
  status: string
  exitCode: number
  msg: string
}

export interface EventTimes {
  CheckActionCache: CheckActionCache
  ComputeMerkleTree: ComputeMerkleTree
  ExecuteRemotely: ExecuteRemotely
  UploadInputs: UploadInputs
}

export interface CheckActionCache {
  from: string
  to: string
}

export interface ComputeMerkleTree {
  from: string
  to: string
}

export interface ExecuteRemotely {
  from: string
  to: string
}

export interface UploadInputs {
  from: string
  to: string
}

export interface LocalMetadata {
  eventTimes: EventTimes2
  labels: Labels
}

export interface EventTimes2 {
  ProcessInputs: ProcessInputs
  ProxyExecution: ProxyExecution
  WrapperOverhead: WrapperOverhead
}

export interface ProcessInputs {
  from: string
  to: string
}

export interface ProxyExecution {
  from: string
  to: string
}

export interface WrapperOverhead {
  from: string
  to: string
}

export interface Labels {
  type: string
}

export class LogRecordDataSource implements DataSource<LogRecord> {

  private recordSubject = new BehaviorSubject<LogRecord[]>([]);
  private loadingSubject = new BehaviorSubject<boolean>(false);

  public loading$ = this.loadingSubject.asObservable();

  constructor(private service: DataService) { }

  connect(collectionViewer: CollectionViewer): Observable<LogRecord[]> {
    return this.recordSubject;
  }

  disconnect(collectionViewer: CollectionViewer): void {
    this.recordSubject.complete();
    this.loadingSubject.complete();
  }

  // =========================
  // BEGIN Filter functions
  // =========================

  private readonly _filter = new BehaviorSubject<string>('');
  filteredData: LogRecord[] = [];

  filterPredicate: (data: LogRecord, filter: string) => boolean = (data: LogRecord, filter: string): boolean => {
    const dataStr = JSON.stringify(data).toLowerCase();
    const transformedFilter = filter.trim().toLowerCase();
    return dataStr.indexOf(transformedFilter) != -1;
  };

  get filter(): string {
    return this._filter.value;
  }

  set filter(filter: string) {
    this._filter.next(filter);
  }

  _filterData(data: LogRecord[]) {
    this.filteredData =
      this.filter == null || this.filter === ''
        ? data
        : data.filter(obj => this.filterPredicate(obj, this.filter));
    for (let i = 0; i < this.filteredData.length; i++) {
      this.filteredData[i].index = i + 1;
    }
    return this.filteredData;
  }

  // =========================
  // END Filter functions
  // =========================


  loadData() {
    this.loadingSubject.next(true);
    const logRecords = this.service.getLogRecords().pipe(
      catchError(() => of([])),
      finalize(() => this.loadingSubject.next(false))
    );
    const curFilteredData = combineLatest([logRecords, this._filter]).pipe(
      map(([data]) => this._filterData(data)));

    curFilteredData.subscribe(data => this.recordSubject.next(data));
  }
}

@Injectable({
  providedIn: 'root'
})
export class DataService {

  constructor(private httpClient: HttpClient) { }

  getLogRecords(): Observable<LogRecord[]> {
    let res = this.httpClient.get<LogRecord[]>("http://localhost:9080/api/data");
    return res;
  }
}
