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
import { NgFor, NgIf, CommonModule } from '@angular/common';
import { Component, OnInit } from '@angular/core';
import { DataService, LogRecord, LogRecordDataSource } from '../data.service';
import { MatProgressSpinnerModule } from '@angular/material/progress-spinner';
import { MatTableModule } from '@angular/material/table';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatIconModule } from '@angular/material/icon';
import { animate, state, style, transition, trigger } from '@angular/animations';

@Component({
  selector: 'app-data-table',
  standalone: true,
  animations: [
    trigger('detailExpand', [
      state('collapsed,void', style({ height: '0px', minHeight: '0' })),
      state('expanded', style({ height: '*' })),
      transition('expanded <=> collapsed', animate('225ms cubic-bezier(0.4, 0.0, 0.2, 1)')),
    ]),
  ],
  imports: [
    NgFor,
    NgIf,
    CommonModule,
    MatProgressSpinnerModule,
    MatTableModule,
    MatFormFieldModule,
    MatInputModule,
    MatIconModule,
  ],
  templateUrl: './data-table.component.html',
  styleUrl: './data-table.component.css'
})
export class DataTableComponent implements OnInit {
  displayedColumns: string[] = [
    'position',
    'commandId',
    'digest',
    'outputs',
  ];
  columnsToDisplayWithExpand = [...this.displayedColumns, 'expand'];
  expandedElement: LogRecord | null | undefined;

  console = console;
  JSON = JSON;

  dataSource!: LogRecordDataSource;

  constructor(private dataService: DataService) {
  }

  ngOnInit() {
    this.dataSource = new LogRecordDataSource(this.dataService);
    this.dataSource.loadData();
  }

  applyFilter(event: Event) {
    const filterValue = (event.target as HTMLInputElement).value;
    this.dataSource.filter = filterValue.trim().toLowerCase();
  }
}
