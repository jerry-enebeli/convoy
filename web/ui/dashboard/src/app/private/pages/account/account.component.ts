import { Component, OnInit } from '@angular/core';
import { FormBuilder, FormGroup, Validators } from '@angular/forms';
import { ActivatedRoute, Router } from '@angular/router';
import { GeneralService } from 'src/app/services/general/general.service';
import { AccountService } from './account.service';

@Component({
	selector: 'app-account',
	templateUrl: './account.component.html',
	styleUrls: ['./account.component.scss']
})
export class AccountComponent implements OnInit {
	activePage: 'profile' | 'security' = 'profile';
	userSettingsMenu: ['profile', 'security'] = ['profile', 'security'];
	isSavingUserDetails = false;
	isUpdatingPassword = false;
	isFetchingUserDetails = false;
	userId!: string;
	passwordToggle = { oldPassword: false, newPassword: false, confirmPassword: false };
	editBasicInfoForm: FormGroup = this.formBuilder.group({
		first_name: ['', Validators.required],
		last_name: ['', Validators.required],
		email: ['', Validators.compose([Validators.required, Validators.email])]
	});
	changePasswordForm: FormGroup = this.formBuilder.group({
		current_password: ['', Validators.required],
		password: ['', Validators.required],
		password_confirmation: ['', Validators.required]
	});
	constructor(private accountService: AccountService, private router: Router, private formBuilder: FormBuilder, private generalService: GeneralService, private route: ActivatedRoute) {}

	ngOnInit() {
		this.toggleActivePage(this.route.snapshot.queryParams?.activePage ?? 'profile');
		this.getAuthDetails();
	}

	getAuthDetails() {
		const authDetails = localStorage.getItem('CONVOY_AUTH');
		if (authDetails && authDetails !== 'undefined') {
			const userId = JSON.parse(authDetails)?.uid;
			this.getUserDetails(userId);
		} else {
			this.router.navigateByUrl('/login');
		}
	}

	async getUserDetails(userId: string) {
		this.isFetchingUserDetails = true;

		try {
			const response = await this.accountService.getUserDetails({ userId: userId });
			this.userId = response.data?.uid;
			this.editBasicInfoForm.patchValue({
				first_name: response.data?.first_name,
				last_name: response.data?.last_name,
				email: response.data?.email
			});
			this.isFetchingUserDetails = false;
		} catch {
			this.isFetchingUserDetails = false;
		}
	}
	async logout() {
		await this.accountService.logout();
		localStorage.removeItem('CONVOY_AUTH');
		this.router.navigateByUrl('/login');
	}

	async editBasicUserInfo() {
		if (this.editBasicInfoForm.invalid) return this.editBasicInfoForm.markAllAsTouched();
		this.isSavingUserDetails = true;
		try {
			const response = await this.accountService.editBasicInfo({ userId: this.userId, body: this.editBasicInfoForm.value });
			this.generalService.showNotification({ style: 'success', message: 'Changes saved successfully!' });
			this.getUserDetails(this.userId);
			this.isSavingUserDetails = false;
		} catch {
			this.isSavingUserDetails = false;
		}
	}

	async changePassword() {
		if (this.changePasswordForm.invalid) return this.changePasswordForm.markAllAsTouched();
		this.isUpdatingPassword = true;
		try {
			const response = await this.accountService.changePassword({ userId: this.userId, body: this.changePasswordForm.value });
			this.generalService.showNotification({ style: 'success', message: response.message });
			this.changePasswordForm.reset();
			this.isUpdatingPassword = false;
		} catch {
			this.isUpdatingPassword = false;
		}
	}

	checkPassword(): boolean {
		const newPassword = this.changePasswordForm.value.password;
		const confirmPassword = this.changePasswordForm.value.password_confirmation;
		if (newPassword === confirmPassword) return true;
		return false;
	}

	toggleActivePage(activePage: 'profile' | 'security') {
		this.activePage = activePage;
		if (!this.router.url.split('/')[2]) this.addPageToUrl();
	}

	addPageToUrl() {
		const queryParams: any = {};
		queryParams.activePage = this.activePage;
		this.router.navigate([], { queryParams: Object.assign({}, queryParams) });
	}
}
