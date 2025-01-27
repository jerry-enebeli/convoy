import { Component, HostListener, OnInit } from '@angular/core';
import { ActivatedRoute } from '@angular/router';
import { GROUP } from 'src/app/models/group.model';
import { PrivateService } from '../../private.service';
import { ProjectService } from './project.service';

@Component({
	selector: 'app-project',
	templateUrl: './project.component.html',
	styleUrls: ['./project.component.scss']
})
export class ProjectComponent implements OnInit {
	screenWidth = window.innerWidth;
	sideBarItems = [
		{
			name: 'Events',
			icon: 'events',
			route: '/events'
		},
		{
			name: 'Sources',
			icon: 'sources',
			route: '/sources'
		},
		{
			name: 'Subscriptions',
			icon: 'subscriptions',
			route: '/subscriptions'
		},
		{
			name: 'Apps',
			icon: 'apps',
			route: '/apps'
		}
	];
	shouldShowFullSideBar = true;
	projectDetails!: GROUP;
	isLoadingProjectDetails: boolean = true;
	showHelpDropdown = false;

	constructor(private route: ActivatedRoute, private privateService: PrivateService) {
		const uid = { uid: this.route.snapshot.params.id };
		this.privateService.activeProjectDetails = { ...this.privateService.activeProjectDetails, ...uid };
	}

	ngOnInit() {
		this.checkScreenSize();
		this.getProjectDetails();
	}

	async getProjectDetails() {
		this.isLoadingProjectDetails = true;

		try {
			const projectDetails = await this.privateService.getProjectDetails();
			this.projectDetails = projectDetails.data;
			localStorage.setItem('PROJECT_CONFIG', JSON.stringify(projectDetails.data?.config));
			if (this.projectDetails.type === 'outgoing') this.sideBarItems.splice(1, 1);
			this.isLoadingProjectDetails = false;
		} catch (error) {
			this.isLoadingProjectDetails = false;
		}
	}

	isOutgoingProject(): boolean {
		return this.projectDetails.type === 'outgoing';
	}

	checkScreenSize() {
		this.screenWidth > 1150 ? (this.shouldShowFullSideBar = true) : (this.shouldShowFullSideBar = false);
	}

	@HostListener('window:resize', ['$event'])
	onWindowResize() {
		this.screenWidth = window.innerWidth;
		this.checkScreenSize();
	}
}
